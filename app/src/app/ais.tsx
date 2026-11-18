// Manage AIs (설정 > 묻기 > AI 관리, #20 decision 7): the AIs the current
// PC found, each with its version, sign-in and plan's usage, and a switch
// that keeps it in or out of the AIs that answer and the moderators (the
// ask prefs' off list, on this phone only); a row opens what the AI can
// do, its tier models and where its questions go. The AIs Askorus knows
// that are not on the PC follow, with the install guide. Adding an AI is
// installing it on the PC and signing in: the PC finds it by itself.

import { router, useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';
import { Linking, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName, aiGuideUrl, loginDay, noticeIsNote, noticeText, supportedAIs } from '@/api/agents';
import { type AdapterInfo, type Info, tiers } from '@/api/types';
import { modelLabel } from '@/ask/models';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { whenAgain } from '@/ask/present';
import { pcLabel } from '@/connections/label';
import { has, type MessageKey, t } from '@/i18n';
import { Avatar } from '@/ui/Avatar';
import { PillChoice } from '@/ui/controls';
import { TopBar } from '@/ui/Header';
import { useSafeInsets } from '@/ui/keyboard';
import { Group, Row, SectionLabel } from '@/ui/List';
import { Sheet } from '@/ui/Sheet';
import { currentPC, usePCs } from '@/ui/shell/pcs';
import { type Theme, themed, useTheme } from '@/ui/theme';

/** percent is a usage share as a whole percent. */
const percent = (left: number) => Math.round(Math.min(1, Math.max(0, left)) * 100);

/** canAnswer: the AI is installed and reports the ask capability. */
const canAnswer = (a: AdapterInfo) => a.available && a.capabilities.includes('ask');

/** signedOut: the PC says the AI is not signed in. */
const signedOut = (a: AdapterInfo) => (a.notices ?? []).some((n) => n.endsWith('_signed_out'));

/** statusLine is the row's second line: version, sign-in, and the usage left when the AI says. */
export function statusLine(a: AdapterInfo): { text: string; bad: boolean } {
  if (!a.available) {
    const n = a.notices?.[0];
    return { text: n ? noticeText(n, a) : t('ais.notReady'), bad: true };
  }
  const parts = [a.version ?? ''];
  const out = signedOut(a);
  parts.push(out ? t('ais.signedOut') : t('ais.signedIn'));
  const main = a.usage?.[0];
  if (!out && main) parts.push(t('ais.usageLeft', { pct: percent(main.left) }));
  return { text: parts.filter(Boolean).join(' · '), bad: out };
}

/** features lists what the AI does, from what /v1/info says (no field of its own, #20 S3). */
export function featuresOf(a: AdapterInfo): { key: string; on: boolean }[] {
  const ask = canAnswer(a);
  const photos = !!a.ask?.images && (a.ask.images.all || (a.ask.images.models ?? []).length > 0);
  return [
    { key: 'ask', on: ask },
    { key: 'follow', on: ask },
    { key: 'moderator', on: a.capabilities.includes('synthesize') },
    { key: 'debate', on: ask },
    { key: 'photos', on: ask && photos },
  ];
}

export default function ManageAIs() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { list, current, pick } = usePCs();
  const [info, setInfo] = useState<Info | null>(null);
  const [failed, setFailed] = useState(false);
  const [ask, setAsk] = useState<AskPrefs>(defaultAskPrefs);
  const [open, setOpen] = useState<AdapterInfo | null>(null);

  const load = useCallback(() => {
    loadAskPrefs().then(setAsk);
    setFailed(false);
    currentPC()
      .then((p) => (p ? p.client.info() : null))
      .then(setInfo)
      .catch(() => {
        setInfo(null);
        setFailed(true);
      });
  }, []);
  useFocusEffect(
    useCallback(() => {
      load();
    }, [load, current?.id]),
  );

  const toggle = (agent: string) => {
    const off = ask.off.includes(agent) ? ask.off.filter((x) => x !== agent) : [...ask.off, agent];
    const next = { ...ask, off };
    setAsk(next);
    saveAskPrefs(next);
  };

  const found = (info?.agents ?? []).filter((a) => a.available || (a.notices ?? []).some((n) => !n.endsWith('_not_found')));
  const missing = supportedAIs.filter((x) => !found.some((a) => a.agent === x));

  return (
    <View style={s.screen}>
      <TopBar title={t('ais.title')} onBack={() => router.back()} />
      <ScrollView style={s.screen} contentContainerStyle={[s.body, { paddingBottom: 24 + insets.bottom }]}>
        {(list?.length ?? 0) > 1 && current ? (
          <PillChoice
            options={(list ?? []).map((c) => ({ value: c.id, label: pcLabel(c, list) }))}
            value={current.id}
            onChange={(id) => void pick(id)}
            label={t('ais.pc')}
          />
        ) : null}
        {!list?.length ? <Text style={s.caption}>{t('ais.noPC')}</Text> : null}
        {failed ? <Text style={s.caption}>{t('ais.loadFailed')}</Text> : null}

        {found.length ? (
          <View style={s.section}>
            <SectionLabel text={current ? t('ais.foundOn', { pc: current.name }) : t('ais.found')} />
            <Group>
              {found.map((a) => {
                const st = statusLine(a);
                const on = !ask.off.includes(a.agent);
                return (
                  <Row
                    key={a.agent}
                    title={agentName(a.agent)}
                    subtitle={st.text}
                    subtitleTone={st.bad ? 'danger' : 'sub'}
                    left={<Avatar agent={a.agent} size={28} />}
                    right={<MiniSwitch on={on} label={t('ais.useFor', { name: agentName(a.agent) })} onChange={() => toggle(a.agent)} th={th} />}
                    minHeight={60}
                    onPress={() => setOpen(a)}
                    accessibilityLabel={agentName(a.agent)}
                  />
                );
              })}
            </Group>
          </View>
        ) : null}

        {info && missing.length ? (
          <View style={s.section}>
            <SectionLabel text={t('ais.missing')} />
            <Group>
              {missing.map((x) => (
                <Row
                  key={x}
                  title={agentName(x)}
                  subtitle={t('ais.notInstalled')}
                  subtitleTone="sub"
                  left={<Avatar agent={x} size={28} tone="dim" />}
                  right={<Text style={s.link}>{t('ais.installGuide')}</Text>}
                  minHeight={60}
                  onPress={() => void Linking.openURL(aiGuideUrl())}
                />
              ))}
            </Group>
          </View>
        ) : null}

        {info ? (
          <View style={s.notes}>
            <Text style={s.caption}>{t('ais.noteOff')}</Text>
            <Text style={s.caption}>{t('ais.noteAdd')}</Text>
          </View>
        ) : null}
      </ScrollView>

      <Sheet open={!!open} onClose={() => setOpen(null)} title={open ? agentName(open.agent) : ''} testID="ai-sheet">
        {open ? <Details a={open} /> : null}
      </Sheet>
    </View>
  );
}

/** MiniSwitch is the row's own switch: it turns the AI on or off without opening the row. */
function MiniSwitch({ on, label, onChange, th }: { on: boolean; label: string; onChange: () => void; th: Theme }) {
  const s = styles(th);
  return (
    <Pressable
      onPress={onChange}
      hitSlop={10}
      accessibilityRole="switch"
      accessibilityLabel={label}
      accessibilityState={{ checked: on }}
      style={[s.track, { backgroundColor: on ? th.fill : th.stopped, justifyContent: on ? 'flex-end' : 'flex-start' }]}
    >
      <View style={[s.knob, { backgroundColor: on ? th.onFill : th.bg }]} />
    </Pressable>
  );
}

/** Details is the sheet of one AI. */
function Details({ a }: { a: AdapterInfo }) {
  const th = useTheme();
  const s = styles(th);
  const out = signedOut(a);
  const expiring = (a.notices ?? []).find(noticeIsNote);
  const login = out ? t('ais.signedOut') : expiring ? t('ais.loginEnds', { date: loginDay(a.loginExpiresAt) }) : t('ais.signedIn');
  const goes = `ais.goesTo.${a.agent}`;
  return (
    <View style={s.sheet}>
      <Group>
        {a.version ? <Row title={t('ais.version')} right={<Text style={s.value}>{a.version}</Text>} /> : null}
        {a.available ? <Row title={t('ais.login')} right={<Text style={[s.value, out && s.bad]}>{login}</Text>} /> : null}
        {(a.usage ?? []).map((u) => (
          <Row
            key={u.name}
            title={u.name}
            subtitle={t('ais.usageLeftLong', { pct: percent(u.left) })}
            subtitleTone="sub"
            right={u.resetAt ? <Text style={s.value}>{t('ais.resets', { time: whenAgain(u.resetAt) })}</Text> : undefined}
          />
        ))}
        {tiers.map((x) => {
          const m = a.ask?.tiers?.[x];
          return m?.model ? <Row key={x} title={t(`ask.tier.${x}`)} right={<Text style={s.value}>{modelLabel(m)}</Text>} /> : null;
        })}
      </Group>
      <Text style={s.label}>{t('ais.features')}</Text>
      <View style={s.chips}>
        {featuresOf(a).map((f) => (
          <View key={f.key} style={[s.chip, !f.on && s.chipOff]} accessibilityLabel={`${t(`ais.feature.${f.key}` as MessageKey)}: ${f.on ? t('ais.yes') : t('ais.no')}`}>
            <Text style={[s.chipText, !f.on && s.chipTextOff]}>{f.on ? t(`ais.feature.${f.key}` as MessageKey) : t(`ais.featureNo.${f.key}` as MessageKey)}</Text>
          </View>
        ))}
      </View>
      {has(goes) ? <Text style={s.caption}>{t(goes as MessageKey)}</Text> : null}
      <Pressable accessibilityRole="button" onPress={() => void Linking.openURL(aiGuideUrl())} style={({ pressed }) => [s.button, pressed && s.pressed]}>
        <Text style={s.buttonText}>{t('ais.openGuide')}</Text>
      </Pressable>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { gap: 14, paddingTop: 4, paddingHorizontal: 16 },
    section: { gap: 6 },
    notes: { gap: 8 },
    caption: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
    link: { fontSize: 14, lineHeight: 20, fontWeight: '500', color: th.accent },
    value: { maxWidth: 190, fontSize: 14, lineHeight: 20, color: th.textSub, textAlign: 'right' },
    bad: { color: th.danger },
    track: { width: 44, height: 26, borderRadius: 13, padding: 3, flexDirection: 'row', alignItems: 'center' },
    knob: { width: 20, height: 20, borderRadius: 10 },
    sheet: { gap: 12, paddingHorizontal: 16, paddingBottom: 12 },
    label: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, paddingHorizontal: 4 },
    chip: { paddingVertical: 5, paddingHorizontal: 12, borderRadius: 14, backgroundColor: th.soft, borderWidth: 1, borderColor: th.softLine },
    chipOff: { backgroundColor: 'transparent', borderColor: th.borderStrong, borderStyle: 'dashed' },
    chipText: { fontSize: 13, lineHeight: 18, color: th.text },
    chipTextOff: { color: th.caption },
    button: { minHeight: 44, borderRadius: 10, alignItems: 'center', justifyContent: 'center', borderWidth: 1.5, borderColor: th.accent },
    pressed: { opacity: 0.6 },
    buttonText: { fontSize: 15, fontWeight: '600', color: th.text },
  }),
);
