// Screen tops (S5c). Header is the two entrances' top (F-Ask-Empty, F-PC):
// the menu button that opens the drawer, the switch '한 번에 | 내 PC' in the
// middle (with a dot on 내 PC while a session of the current PC waits for
// input and 한 번에 is the one shown: on 내 PC its list says who waits, and
// the mark on the picked pill would be 1.4:1 in dark, UX-5), and a button
// at the right. TopBar is the top of the screens
// stacked on them: back, a centred title, or the app's name (F-Pair).

import { router } from 'expo-router';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';

import { Icon, IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { usePCStatus } from './shell/pcStatus';
import { useShell } from './shell/Shell';
import { themed, useTheme } from './theme';

export type Entrance = 'ask' | 'pcs';
export const entranceHref: Record<Entrance, '/' | '/pcs'> = { ask: '/', pcs: '/pcs' };

const entrances: readonly Entrance[] = ['ask', 'pcs'];

export function Header({ entrance, right }: { entrance: Entrance; right?: ReactNode }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { openDrawer } = useShell();
  const waiting = usePCStatus()?.waiting.length ?? 0;
  return (
    <View style={{ paddingTop: insets.top, backgroundColor: th.bg }}>
      <View style={s.bar}>
        <IconButton name="menu" label={t('nav.menu')} onPress={openDrawer} size={22} />
        <View accessibilityRole="tablist" accessibilityLabel={t('nav.entrances')} style={s.track}>
          {entrances.map((k) => {
            const on = k === entrance;
            const name = k === 'ask' ? t('tab.ask') : t('tab.pcs');
            const dot = k === 'pcs' && !on && waiting > 0;
            return (
              <Pressable
                key={k}
                onPress={() => {
                  if (!on) router.navigate(entranceHref[k]);
                }}
                accessibilityRole="tab"
                accessibilityLabel={dot ? `${name}, ${t('nav.waiting', { n: waiting })}` : name}
                accessibilityState={{ selected: on }}
                hitSlop={{ top: 7, bottom: 7 }}
                style={[s.seg, on ? s.segOn : s.segOff]}
              >
                <Icon name={k === 'ask' ? 'ask' : 'pc'} size={16} color={on ? th.onSelected : th.textSub} />
                <Text style={[s.segText, on ? s.segTextOn : s.segTextOff]} numberOfLines={1}>
                  {name}
                </Text>
                {dot ? <View style={s.dot} testID="waiting-dot" /> : null}
              </Pressable>
            );
          })}
        </View>
        {right ?? <View style={s.spacer} />}
      </View>
    </View>
  );
}

export function TopBar({ title, subtitle, onBack, right, brand }: { title?: string; subtitle?: string; onBack?: () => void; right?: ReactNode; brand?: boolean }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const back = onBack ? <IconButton name="back" label={t('nav.back')} onPress={onBack} size={22} /> : null;
  return (
    <View style={{ paddingTop: insets.top, backgroundColor: th.bg }}>
      <View style={s.bar}>
        {brand ? (
          <View style={s.brand}>
            {back}
            <Text style={[s.title, { marginLeft: back ? 2 : 14 }]} accessibilityRole="header">
              {t('app.name')}
            </Text>
          </View>
        ) : (
          (back ?? <View style={s.spacer} />)
        )}
        {!brand && (title || subtitle) ? (
          <View style={s.centre} pointerEvents="none">
            {title ? (
              <Text style={s.title} numberOfLines={1} accessibilityRole="header">
                {title}
              </Text>
            ) : null}
            {subtitle ? (
              <Text style={s.subtitle} numberOfLines={1}>
                {subtitle}
              </Text>
            ) : null}
          </View>
        ) : null}
        {right ?? <View style={s.spacer} />}
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    bar: { height: 56, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 6 },
    spacer: { width: 44, height: 44 },
    track: {
      height: 36,
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 3,
      borderRadius: 18,
      backgroundColor: th.segTrack,
      borderWidth: 1,
      borderColor: th.segLine,
    },
    seg: { height: 30, borderRadius: 15, flexDirection: 'row', alignItems: 'center', gap: 6 },
    segOn: { backgroundColor: th.selected, paddingLeft: 12, paddingRight: 14 },
    segOff: { paddingHorizontal: 12 },
    segText: { fontSize: 14, lineHeight: 20 },
    segTextOn: { fontWeight: '600', color: th.onSelected },
    segTextOff: { fontWeight: '500', color: th.textSub },
    dot: { width: 7, height: 7, borderRadius: 3.5, backgroundColor: th.mark },
    brand: { flexDirection: 'row', alignItems: 'center' },
    centre: { position: 'absolute', left: 56, right: 56, top: 0, bottom: 0, alignItems: 'center', justifyContent: 'center', gap: 1 },
    title: { fontSize: 17, fontWeight: '600', lineHeight: 23, letterSpacing: -0.17, color: th.text },
    subtitle: { fontSize: 13, lineHeight: 18, color: th.caption },
  }),
);
