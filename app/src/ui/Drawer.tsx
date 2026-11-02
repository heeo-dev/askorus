// The drawer (S5c, F-Drawer): a panel from the left with this run's
// questions by day, the current PC with its sessions waiting for input,
// Settings and the not-official line. It is a plain Modal panel rather than
// a navigation drawer, so no gesture or animation library comes in; it
// opens from the header's menu button only (no edge swipe). It closes
// before it navigates, and slides out while the next screen shows.

import { router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Animated, Modal, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import { conversations, dayGroups, useConversations } from '@/ask/conversations';
import { label, t } from '@/i18n';

import { StatusDot } from './controls';
import { sessionTitle } from './format';
import { Icon, IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { usePCs } from './shell/pcs';
import { usePCStatus } from './shell/pcStatus';
import { themed, useTheme } from './theme';

export function Drawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { width } = useWindowDimensions();
  const panel = Math.min(328, width - 62);
  const [shown, setShown] = useState(open);
  const progress = useRef(new Animated.Value(open ? 1 : 0)).current;
  const { list, current } = useConversations();
  const pcs = usePCs();
  const status = usePCStatus();

  useEffect(() => {
    if (open) setShown(true);
    const run = Animated.timing(progress, { toValue: open ? 1 : 0, duration: 200, useNativeDriver: true });
    run.start(({ finished }) => {
      if (finished && !open) setShown(false);
    });
    return () => run.stop();
  }, [open, progress]);

  if (!open && !shown) return null;

  const go = (f: () => void) => {
    onClose();
    f();
  };
  const groups = dayGroups({ list, current, now: new Date() });
  const pc = pcs.current;
  const state = !status || status.state === 'checking' ? 'checking' : status.state;
  const waiting = state === 'ok' && status ? status.waiting : [];
  const pcLine = state === 'ok' ? t('pc.connected') : state === 'error' ? t('pc.notConnected') : t('home.checking');

  return (
    <Modal transparent visible animationType="none" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <View style={s.screen}>
        <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: th.drawerScrim, opacity: progress }]}>
          <Pressable
            style={StyleSheet.absoluteFill}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel={t('nav.menuClose')}
            testID="drawer-scrim"
          />
        </Animated.View>
        <Animated.View
          accessibilityViewIsModal
          testID="drawer"
          style={[
            s.panel,
            {
              width: panel,
              paddingTop: insets.top,
              paddingBottom: 12 + insets.bottom,
              transform: [{ translateX: progress.interpolate({ inputRange: [0, 1], outputRange: [-panel - 40, 0] }) }],
            },
          ]}
        >
          <View style={s.head}>
            <Text style={s.title} accessibilityRole="header">
              {t('app.name')}
            </Text>
            <IconButton name="close" label={t('nav.menuClose')} onPress={onClose} />
          </View>

          <ScrollView style={s.history} accessibilityLabel={t('drawer.history')}>
            {groups.length === 0 ? <Text style={s.empty}>{t('drawer.empty')}</Text> : null}
            {groups.map((g, gi) => (
              <View key={g.key} style={gi ? s.dayGap : undefined}>
                <Text style={s.day} accessibilityRole="header">
                  {g.label}
                </Text>
                {g.items.map((item) => (
                  <Pressable
                    key={item.id}
                    onPress={() =>
                      go(() => {
                        if (!item.current) conversations.requestOpen(item.id);
                        router.navigate('/');
                      })
                    }
                    accessibilityRole="button"
                    accessibilityLabel={item.title}
                    accessibilityState={{ selected: item.current }}
                    style={({ pressed }) => [s.item, item.current && s.itemOn, pressed && !item.current && s.pressed]}
                  >
                    <Text style={[s.itemText, item.current && s.itemTextOn]} numberOfLines={1}>
                      {item.title}
                    </Text>
                  </Pressable>
                ))}
              </View>
            ))}
          </ScrollView>

          {pcs.list ? (
            <View style={s.pcSection}>
              <Text style={[s.day, s.pcLabel]} accessibilityRole="header">
                {t('tab.pcs')}
              </Text>
              <View style={s.pcBlock}>
                {pc ? (
                  <Pressable
                    onPress={() => go(() => router.navigate('/pcs'))}
                    accessibilityRole="button"
                    accessibilityLabel={`${pc.name}, ${pcLine}`}
                    style={({ pressed }) => [s.pcRow, pressed && s.pressed]}
                  >
                    <Icon name="pc" size={20} color={th.textSub} />
                    <Text style={s.pcName} numberOfLines={1}>
                      {pc.name}
                    </Text>
                    <View style={s.pcState}>
                      <StatusDot kind={state === 'ok' ? 'ok' : state === 'error' ? 'failed' : 'stopped'} size={7} />
                      <Text style={s.pcStateText}>{pcLine}</Text>
                    </View>
                    <Icon name="chevronRight" size={18} color={th.textSub} />
                  </Pressable>
                ) : (
                  <Pressable
                    onPress={() => go(() => router.push('/pair'))}
                    accessibilityRole="button"
                    accessibilityLabel={t('home.pair')}
                    style={({ pressed }) => [s.pcRow, pressed && s.pressed]}
                  >
                    <Icon name="plus" size={20} color={th.accent} />
                    <Text style={[s.pcName, { color: th.accent }]}>{t('home.pair')}</Text>
                  </Pressable>
                )}
                {waiting.map((x) => {
                  const name = sessionTitle(x);
                  const what = label('activity', 'blocked');
                  return (
                    <Pressable
                      key={x.id}
                      onPress={() => go(() => router.navigate({ pathname: '/pcs', params: { session: x.id } }))}
                      accessibilityRole="button"
                      accessibilityLabel={`${name}, ${what}`}
                      style={({ pressed }) => [s.waitRow, pressed && s.pressed]}
                    >
                      <View style={s.waitDot}>
                        <StatusDot kind="waiting" size={10} halo />
                      </View>
                      <View style={s.waitText}>
                        <Text style={s.pcName} numberOfLines={1}>
                          {name}
                        </Text>
                        <Text style={s.waitLine}>{what}</Text>
                      </View>
                      <Icon name="chevronRight" size={18} color={th.textSub} />
                    </Pressable>
                  );
                })}
              </View>
            </View>
          ) : null}

          <View style={s.foot}>
            <Pressable
              onPress={() => go(() => router.push('/settings'))}
              accessibilityRole="button"
              accessibilityLabel={t('settings.title')}
              style={({ pressed }) => [s.settings, pressed && s.pressed]}
            >
              <Icon name="settings" size={22} color={th.textSub} />
              <Text style={s.settingsText}>{t('settings.title')}</Text>
            </Pressable>
            <Text style={s.note}>{t('app.notOfficial')}</Text>
          </View>
        </Animated.View>
      </View>
    </Modal>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1 },
    panel: {
      position: 'absolute',
      top: 0,
      bottom: 0,
      left: 0,
      paddingHorizontal: 12,
      backgroundColor: th.surface,
      borderTopRightRadius: 20,
      borderBottomRightRadius: 20,
      boxShadow: '8px 0px 32px rgba(0,0,0,0.4)',
    },
    pressed: { backgroundColor: th.raised },
    head: { height: 56, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingLeft: 12 },
    title: { fontSize: 17, fontWeight: '600', lineHeight: 23, letterSpacing: -0.17, color: th.text },
    history: { flex: 1 },
    empty: { paddingHorizontal: 12, paddingVertical: 8, fontSize: 13, lineHeight: 19.5, color: th.caption },
    dayGap: { marginTop: 8 },
    day: { height: 32, paddingHorizontal: 12, fontSize: 13, fontWeight: '500', lineHeight: 32, color: th.caption },
    item: { minHeight: 44, justifyContent: 'center', paddingHorizontal: 12, borderRadius: 22 },
    itemOn: { backgroundColor: th.raised },
    itemText: { fontSize: 15, lineHeight: 21, color: th.text },
    itemTextOn: { fontWeight: '500' },
    pcSection: { gap: 6, paddingTop: 12, borderTopWidth: 1, borderTopColor: th.divider },
    pcLabel: { height: 24, lineHeight: 24 },
    pcBlock: { paddingVertical: 2, borderRadius: 18, backgroundColor: th.raised, overflow: 'hidden' },
    pcRow: { minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 14, paddingRight: 10 },
    pcName: { flexShrink: 1, flexGrow: 1, fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    pcState: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    pcStateText: { fontSize: 13, lineHeight: 18, color: th.textSub },
    waitRow: {
      minHeight: 60,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 12,
      paddingLeft: 14,
      paddingRight: 10,
      borderTopWidth: 1,
      borderTopColor: th.dividerRaised,
    },
    waitDot: { width: 20, alignItems: 'center' },
    waitText: { flex: 1, minWidth: 0, gap: 2 },
    waitLine: { fontSize: 13, lineHeight: 18, color: th.waitingText },
    foot: { marginTop: 8, paddingTop: 4, borderTopWidth: 1, borderTopColor: th.divider },
    settings: { height: 48, flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 12, borderRadius: 24 },
    settingsText: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    note: { marginTop: 2, paddingHorizontal: 12, fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
