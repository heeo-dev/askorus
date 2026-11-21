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
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs, toggleOff } from '@/ask/prefs';
import { whenAgain } from '@/ask/present';
import { percentLeft, used } from '@/ask/usage';
import { pcLabel } from '@/connections/label';
import { has, type MessageKey, t } from '@/i18n';
import { Avatar } from '@/ui/Avatar';
import { TopBar } from '@/ui/Header';
import { useSafeInsets } from '@/ui/keyboard';
import { Group, Row, SectionLabel } from '@/ui/List';
import { PCPickerSheet } from '@/ui/PCPickerSheet';
import { Sheet } from '@/ui/Sheet';
import { currentPC, usePCs } from '@/ui/shell/pcs';
import { publishInfo } from '@/ui/shell/pcUsage';
import { byWindow, UsageMini, usageWords, windowLabel } from '@/ui/Usage';
import { type Theme, themed, useTheme } from '@/ui/theme';


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
  return { text: parts.filter(Boolean).join(' · '), bad: out };
}

/**
 * features lists what the AI does, from what /v1/info says (no field of its
 * own, #20 S3). newOnly: it does it only in a new question (photos of an AI
 * whose images.newOnly is set, Antigravity, R-130).
 */
export function featuresOf(a: AdapterInfo): { key: string; on: boolean; newOnly?: boolean }[] {
  const ask = canAnswer(a);
  const photos = !!a.ask?.images && (a.ask.images.all || (a.ask.images.models ?? []).length > 0);
  return [
    { key: 'ask', on: ask },
    { key: 'follow', on: ask },
    { key: 'moderator', on: a.capabilities.includes('synthesize') },
    { key: 'debate', on: ask },
    { key: 'photos', on: ask && photos, newOnly: ask && photos && !!a.ask?.images?.newOnly },
    { key: 'make', on: ask && !!a.ask?.makes },
  ];
}

/** togglePictures turns making pictures on or off for agent (#14); on is the default. */
export function togglePictures(p: AskPrefs, agent: string): AskPrefs {
  const off = p.noPictures.includes(agent);
  return { ...p, noPictures: off ? p.noPictures.filter((x) => x !== agent) : [...p.noPictures, agent] };
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
  const [picker, setPicker] = useState(false);

  const load = useCallback(() => {
    loadAskPrefs().then(setAsk);
    setFailed(false);
    currentPC()
      .then(async (p) => {
        if (!p) return null;
        const info = await p.client.info();
        publishInfo(p.conn.id, info); // the top bar's usage button reads it too (#20)
        return info;
      })
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
    const next = toggleOff(ask, agent);
    setAsk(next);
    saveAskPrefs(next);
  };

  const found = (info?.agents ?? []).filter((a) => a.available || (a.notices ?? []).some((n) => !n.endsWith('_not_found')));
  const missing = supportedAIs.filter((x) => !found.some((a) => a.agent === x));

  return (
    <View style={s.screen}>
      <TopBar title={t('ais.title')} onBack={() => router.back()} />
      <ScrollView style={s.screen} contentContainerStyle={[s.body, { paddingBottom: 24 + insets.bottom }]}>
        {/* Several PCs: one row with the PC shown, the PC picker on a press (S10, 11-18: a pill each took half the screen). */}
        {(list?.length ?? 0) > 1 && current ? (
          <Group>
            <Row
              title={t('ais.pc')}
              right={
                <Text style={s.value} numberOfLines={1}>
                  {pcLabel(current, list)}
                </Text>
              }
              chevron
              onPress={() => setPicker(true)}
              accessibilityValue={{ text: pcLabel(current, list) }}
            />
          </Group>
        ) : null}
        {!list?.length ? <Text style={s.caption}>{t('ais.noPC')}</Text> : null}
        {failed ? <Text style={s.caption}>{t('ais.loadFailed')}</Text> : null}

        {found.length ? (
          <View style={s.section}>
            <SectionLabel text={current ? t('ais.foundOn', { pc: current.name }) : t('ais.found')} />
            <Group>
              {found.map((a) => {
                const st = statusLine(a);
                // The usage under it (#20): every window, when the AI is signed in and says.
                const windows = signedOut(a) ? [] : byWindow(a.usage ?? []);
                const on = !ask.off.includes(a.agent);
                return (
                  <Row
                    key={a.agent}
                    title={agentName(a.agent)}
                    subtitle={st.text}
                    subtitleTone={st.bad ? 'danger' : 'sub'}
                    below={windows.length ? windows.map((g) => <UsageMini key={`${g.window ?? ''}${g.name}`} g={g} />) : undefined}
                    accessibilityLabel={[agentName(a.agent), st.text, ...windows.map(usageWords)].filter(Boolean).join(', ')}
                    left={<Avatar agent={a.agent} size={28} />}
                    right={<MiniSwitch on={on} label={t('ais.useFor', { name: agentName(a.agent) })} onChange={() => toggle(a.agent)} th={th} />}
                    minHeight={60}
                    onPress={() => setOpen(a)}
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

      <PCPickerSheet
        open={picker}
        onClose={() => setPicker(false)}
        list={list ?? []}
        currentId={current?.id ?? null}
        onPick={(id) => {
          setPicker(false);
          if (id !== current?.id) void pick(id);
        }}
        onPair={() => {
          setPicker(false);
          router.push('/pair');
        }}
      />
      <Sheet open={!!open} onClose={() => setOpen(null)} title={open ? agentName(open.agent) : ''} testID="ai-sheet">
        {open ? (
          <Details
            a={open}
            pictures={!ask.noPictures.includes(open.agent)}
            onPictures={() => {
              const next = togglePictures(ask, open.agent);
              setAsk(next);
              saveAskPrefs(next);
            }}
          />
        ) : null}
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

/** Details is the sheet of one AI; pictures and onPictures are its picture switch (#14). */
function Details({ a, pictures, onPictures }: { a: AdapterInfo; pictures: boolean; onPictures: () => void }) {
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
            key={`${u.window ?? ''}${u.name}`}
            title={windowLabel(u)}
            subtitle={used(u) ? t('usage.out') : t('usage.left', { pct: percentLeft(u.left) })}
            subtitleTone={used(u) ? 'danger' : 'sub'}
            right={u.resetAt ? <Text style={s.value}>{t('ais.resets', { time: whenAgain(u.resetAt) })}</Text> : undefined}
          />
        ))}
        {a.usage?.[0]?.at ? <Text style={s.asOf}>{t('usage.asOf', { time: whenAgain(a.usage[0].at) })}</Text> : null}
        {tiers.map((x) => {
          const m = a.ask?.tiers?.[x];
          return m?.model ? <Row key={x} title={t(`ask.tier.${x}`)} right={<Text style={s.value}>{modelLabel(m)}</Text>} /> : null;
        })}
      </Group>
      <Text style={s.label}>{t('ais.features')}</Text>
      <View style={s.chips}>
        {featuresOf(a).map((f) => (
          <View
            key={f.key}
            style={[s.chip, !f.on && s.chipOff]}
            accessibilityLabel={`${t(`ais.feature.${f.key}` as MessageKey)}: ${f.newOnly ? t('ais.newOnly') : f.on ? t('ais.yes') : t('ais.no')}`}
          >
            <Text style={[s.chipText, !f.on && s.chipTextOff]}>
              {f.newOnly ? t(`ais.featureNewOnly.${f.key}` as MessageKey) : f.on ? t(`ais.feature.${f.key}` as MessageKey) : t(`ais.featureNo.${f.key}` as MessageKey)}
            </Text>
          </View>
        ))}
      </View>
      {a.ask?.makes ? (
        <View style={s.section}>
          <Group>
            <Row
              title={t('ais.makePictures')}
              subtitle={a.ask.makes.limitedUntil ? t('ais.pictureLimit', { time: whenAgain(a.ask.makes.limitedUntil) }) : undefined}
              subtitleTone={a.ask.makes.limitedUntil ? 'danger' : 'sub'}
              right={<MiniSwitch on={pictures} label={t('ais.makePicturesFor', { name: agentName(a.agent) })} onChange={onPictures} th={th} />}
              onPress={onPictures}
            />
          </Group>
          <Text style={s.caption}>{t('ais.makeNote')}</Text>
        </View>
      ) : null}
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
    asOf: { fontSize: 12.5, lineHeight: 18, color: th.caption, paddingHorizontal: 16, paddingVertical: 8 },
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
