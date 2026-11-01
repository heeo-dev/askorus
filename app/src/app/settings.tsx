// Settings: paired PCs (check the connection, unpair), what new sessions
// start with, the default ask tier, whether answers are compared on their
// own (P2-3), and about the app.

import Constants from 'expo-constants';
import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';
import { ActivityIndicator, Alert, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { AgentError } from '@/api/client';
import { tiers } from '@/api/types';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { type Check, diagnose } from '@/connections/diagnose';
import { connect } from '@/connections/open';
import { type Prefs, efforts, loadPrefs, models, savePrefs } from '@/connections/prefs';
import { type Connection, ConnectionStore } from '@/connections/store';
import { t } from '@/i18n';
import { Button } from '@/ui/Button';
import { Choices } from '@/ui/Choices';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

export default function Settings() {
  const th = useTheme();
  const s = styles(th);
  const [conns, setConns] = useState<Connection[]>([]);
  const [checks, setChecks] = useState<Record<string, Check[] | 'running'>>({});
  const [note, setNote] = useState('');
  const [prefs, setPrefs] = useState<Prefs>({ model: '', effort: '' });
  const [ask, setAsk] = useState<AskPrefs>(defaultAskPrefs);

  useFocusEffect(
    useCallback(() => {
      store.list().then(setConns);
      loadPrefs().then(setPrefs);
      loadAskPrefs().then(setAsk);
    }, []),
  );

  const check = async (c: Connection) => {
    setChecks((m) => ({ ...m, [c.id]: 'running' }));
    const pc = await connect(store, c.id);
    const result = pc ? await diagnose(pc.client) : [];
    setChecks((m) => ({ ...m, [c.id]: result }));
  };

  const unpair = (c: Connection) => {
    Alert.alert(t('settings.unpairTitle', { name: c.name }), t('settings.unpairBody'), [
      { text: t('sheet.cancel'), style: 'cancel' },
      {
        text: t('settings.unpair'),
        style: 'destructive',
        onPress: async () => {
          const pc = await connect(store, c.id);
          let leftOnPC = false;
          try {
            await pc?.client.forgetSelf();
          } catch (e) {
            // Already gone on the PC is fine; otherwise say what is left.
            leftOnPC = !(e instanceof AgentError && e.code === 'unauthorized');
          }
          await store.remove(c.id);
          setConns(await store.list());
          if (leftOnPC) {
            setNote(t('settings.leftOnPC', { id: c.deviceId }));
          }
        },
      },
    ]);
  };

  const change = (next: Prefs) => {
    setPrefs(next);
    savePrefs(next);
  };

  return (
    <ScrollView style={s.screen} contentContainerStyle={s.body}>
      <Text style={s.heading}>{t('settings.pcs')}</Text>
      {conns.length === 0 ? <Text style={s.muted}>{t('home.empty')}</Text> : null}
      {note ? <Text style={s.note}>{note}</Text> : null}
      {conns.map((c) => {
        const ch = checks[c.id];
        return (
          <View key={c.id} style={s.card}>
            <Text style={s.title}>{c.name}</Text>
            <Text style={s.muted}>
              {c.host}:{c.port}
            </Text>
            <Text style={s.muted}>{t('settings.thisPhone', { name: c.deviceName, id: c.deviceId })}</Text>
            {ch === 'running' ? <ActivityIndicator color={th.accent} /> : null}
            {Array.isArray(ch)
              ? ch.map((x, i) => (
                  <Text key={i} style={x.note ? s.muted : x.ok ? s.ok : s.bad}>
                    {x.note ? '· ' : x.ok ? '✓ ' : '✗ '}
                    {x.text}
                  </Text>
                ))
              : null}
            <View style={s.buttons}>
              <Button secondary label={t('settings.check')} onPress={() => check(c)} disabled={ch === 'running'} />
              <Button danger label={t('settings.unpair')} onPress={() => unpair(c)} />
            </View>
          </View>
        );
      })}

      <Text style={s.heading}>{t('settings.defaults')}</Text>
      <Text style={s.label}>{t('new.model')}</Text>
      <Choices
        options={models}
        value={(models as readonly string[]).includes(prefs.model) ? (prefs.model as (typeof models)[number]) : ''}
        onChange={(model) => change({ ...prefs, model })}
        labelOf={(m) => m || t('new.default')}
      />
      <Text style={s.label}>{t('new.effort')}</Text>
      <Choices
        options={efforts}
        value={prefs.effort as (typeof efforts)[number]}
        onChange={(effort) => change({ ...prefs, effort })}
        labelOf={(e) => e || t('new.default')}
      />

      <Text style={s.heading}>{t('settings.askTier')}</Text>
      <Choices
        options={tiers}
        value={ask.tier}
        onChange={(tier) => {
          const next = { ...ask, tier };
          setAsk(next);
          saveAskPrefs(next);
        }}
        labelOf={(x) => t(`ask.tier.${x}`)}
      />
      <Text style={s.muted}>{t('settings.askTierHint')}</Text>

      {/* P2-3. A Pressable rather than Switch, so screen tests can press it (R-07). */}
      <Text style={s.heading}>{t('settings.compare')}</Text>
      <Pressable
        onPress={() => {
          const next = { ...ask, compare: !ask.compare };
          setAsk(next);
          saveAskPrefs(next);
        }}
        style={({ pressed }) => [s.switchRow, pressed && s.pressed]}
        accessibilityRole="switch"
        accessibilityLabel={t('settings.compareAuto')}
        accessibilityState={{ checked: ask.compare }}
      >
        <Text style={s.switchLabel}>{t('settings.compareAuto')}</Text>
        <View style={[s.track, ask.compare && s.trackOn]}>
          <View style={[s.knob, ask.compare && s.knobOn]} />
        </View>
      </Pressable>
      <Text style={s.muted}>{t('settings.compareHint')}</Text>

      <Text style={s.heading}>{t('settings.about')}</Text>
      <Text style={s.muted}>
        {t('app.name')} {Constants.expoConfig?.version ?? ''}
      </Text>
      <Text style={s.muted}>{t('app.notOfficial')}</Text>
    </ScrollView>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { padding: 16, gap: 10, paddingBottom: 40 },
    heading: { color: th.text, fontSize: 18, fontWeight: '700', marginTop: 12 },
    label: { color: th.text, fontSize: 15, fontWeight: '600', marginTop: 4 },
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 14,
      gap: 6,
    },
    title: { color: th.text, fontSize: 16, fontWeight: '600' },
    muted: { color: th.muted, fontSize: 13 },
    note: { color: th.text, fontSize: 14, lineHeight: 20 },
    ok: { color: th.ok, fontSize: 14 },
    bad: { color: th.danger, fontSize: 14 },
    buttons: { gap: 8, marginTop: 6 },
    switchRow: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 },
    switchLabel: { flex: 1, color: th.text, fontSize: 15 },
    // Off in muted, not border: a control's parts need 3:1 against the page (WCAG 1.4.11).
    track: { width: 44, height: 26, borderRadius: 13, padding: 3, backgroundColor: th.muted },
    trackOn: { backgroundColor: th.accent },
    knob: { width: 20, height: 20, borderRadius: 10, backgroundColor: th.card },
    knobOn: { alignSelf: 'flex-end' },
    pressed: { opacity: 0.7 },
  });
