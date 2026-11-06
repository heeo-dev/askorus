// Settings (S5c, board F-Settings): paired PCs (a sheet each to check the
// connection or unpair), what new sessions start with, pairing another PC;
// the default ask tier; whether the moderator writes a combined answer once
// the answers are in (S5b; since C6 the one switch) and which AI it is,
// its model, and whether a follow-up carries the conclusions so far (#29)
// (question refining, milestone C, is hidden while its feature is off:
// features.ts); the theme
// (system, dark or light) and whether each AI has its own colour (#6); the
// conversations kept on the phone, and deleting them all (milestone B); and
// about the app.

import Constants from 'expo-constants';
import { router, useFocusEffect } from 'expo-router';
import { type ReactNode, useCallback, useState } from 'react';
import { Alert, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { tiers } from '@/api/types';
import { conversations, useConversations } from '@/ask/conversations';
import { features } from '@/ask/features';
import { modelLabel } from '@/ask/models';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs, synthWriters } from '@/ask/prefs';
import { type Asker, askers } from '@/ask/thread';
import { loadPrefs, type Prefs, savePrefs } from '@/connections/prefs';
import type { Connection } from '@/connections/store';
import { t } from '@/i18n';
import { PillChoice, Segment, SwitchRow } from '@/ui/controls';
import { DefaultsSheet, settingsLine } from '@/ui/DefaultsSheet';
import { TopBar } from '@/ui/Header';
import { Icon, IconButton } from '@/ui/Icon';
import { useSafeInsets } from '@/ui/keyboard';
import { Group, Row, SectionLabel } from '@/ui/List';
import { PCRow, useReach } from '@/ui/PCPickerSheet';
import { PCSheet } from '@/ui/PCSheet';
import { ModelSheet } from '@/ui/ModelSheet';
import { Sheet } from '@/ui/Sheet';
import { currentPC, usePCs } from '@/ui/shell/pcs';
import { themeChoices, themed, useAIColours, useTheme, useThemeChoice } from '@/ui/theme';

export default function Settings() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { list, reload } = usePCs();
  const reach = useReach(list);
  const { choice, setChoice } = useThemeChoice();
  const aiColours = useAIColours();
  const kept = useConversations().list.length;
  const [note, setNote] = useState('');
  const [prefs, setPrefs] = useState<Prefs>({ model: '', effort: '' });
  const [ask, setAsk] = useState<AskPrefs>(defaultAskPrefs);
  const [pcOpen, setPCOpen] = useState<Connection | null>(null);
  const [defaultsOpen, setDefaultsOpen] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  // The current PC's AIs, for the moderator model's list (11-06); null while loading or without a PC.
  const [pcAIs, setPCAIs] = useState<Asker[] | null>(null);
  const [modelOpen, setModelOpen] = useState(false);

  useFocusEffect(
    useCallback(() => {
      reload().catch(() => undefined);
      loadPrefs().then(setPrefs);
      loadAskPrefs().then(setAsk);
      currentPC()
        .then((p) => (p ? p.client.info() : null))
        .then((info) => setPCAIs(info ? askers(info) : null))
        .catch(() => setPCAIs(null));
    }, [reload]),
  );

  const change = (next: Prefs) => {
    setPrefs(next);
    savePrefs(next);
  };

  const changeAsk = (next: AskPrefs) => {
    setAsk(next);
    saveAskPrefs(next);
  };

  const unpaired = async (c: Connection, leftOnPC: boolean) => {
    setPCOpen(null);
    await reload();
    setNote(leftOnPC ? t('settings.leftOnPC', { id: c.deviceId }) : '');
  };

  const defaults = settingsLine(prefs.model, prefs.effort);
  // The moderator model (11-06): set per AI; the list is the moderator AI's on the current PC.
  const moderator = pcAIs?.find((a) => a.agent === ask.synthAgent && a.canAsk) ?? null;
  const moderatorPick = ask.synthModels[ask.synthAgent] ?? null;
  const moderatorValue = moderatorPick ? modelLabel(moderatorPick) : t('ask.byTier');
  const version = Constants.expoConfig?.version ?? '';

  return (
    <View style={s.screen}>
      <TopBar title={t('settings.title')} onBack={() => router.back()} />
      <ScrollView style={s.screen} contentContainerStyle={[s.body, { paddingBottom: 24 + insets.bottom }]}>
        <Section title={t('settings.pcs')}>
          {list?.length === 0 ? <Text style={s.caption}>{t('home.empty')}</Text> : null}
          {note ? <Text style={s.note}>{note}</Text> : null}
          <Group>
            {(list ?? []).map((c) => (
              <PCRow key={c.id} conn={c} reach={reach[c.id]} long={false} tile={32} onPress={() => setPCOpen(c)} />
            ))}
            <Row
              title={t('settings.defaults')}
              right={<Text style={s.value}>{defaults}</Text>}
              chevron
              onPress={() => setDefaultsOpen(true)}
              accessibilityValue={{ text: defaults }}
            />
            <Row
              title={t('home.pair')}
              left={<Icon name="plus" size={20} color={th.accent} />}
              titleStyle={s.accent}
              onPress={() => router.push('/pair')}
            />
          </Group>
        </Section>

        <Section title={t('settings.ask')}>
          <Group>
            <Row
              title={t('settings.askTierShort')}
              minHeight={56}
              right={
                <Segment
                  options={tiers}
                  value={ask.tier}
                  onChange={(tier) => changeAsk({ ...ask, tier })}
                  labelOf={(x) => t(`ask.tier.${x}`)}
                  look="setting"
                  label={t('settings.askTierShort')}
                />
              }
            />
          </Group>
        </Section>

        <Section
          title={t('settings.compareSynth')}
          right={<IconButton name="info" label={t('settings.compareInfo')} onPress={() => setInfoOpen(true)} size={18} color={th.caption} style={s.info} />}
        >
          <Group>
            {features.refine && <SwitchRow label={t('settings.refineAuto')} value={ask.refine} onChange={(refine) => changeAsk({ ...ask, refine })} />}
            <SwitchRow label={t('settings.synthAutoMod')} value={ask.synth} onChange={(synth) => changeAsk({ ...ask, synth })} />
            {/* S5b. A fixed list of writers: Settings has no PC info, and the PC falls back when it lacks the AI. */}
            <View style={s.writer}>
              <Text style={s.writerLabel}>{t(features.refine ? 'settings.synthAgentLong' : 'settings.moderatorAI')}</Text>
              <PillChoice
                options={synthWriters.map((a) => ({ value: a, label: agentName(a), agent: a }))}
                value={ask.synthAgent as (typeof synthWriters)[number]}
                onChange={(synthAgent) => changeAsk({ ...ask, synthAgent })}
                label={t('settings.synthAgentLong')}
              />
            </View>
            {/* 11-06: the moderator AI's model and effort for the combined answer and debates; its list is the current PC's. */}
            <Row
              title={t('settings.moderatorModel')}
              subtitle={moderator ? undefined : t('settings.moderatorModelNoPC')}
              right={<Text style={s.value}>{moderatorValue}</Text>}
              chevron={!!moderator}
              onPress={moderator ? () => setModelOpen(true) : undefined}
              accessibilityValue={{ text: moderatorValue }}
            />
            {/* #29: a follow-up carries what was concluded so far. */}
            <SwitchRow label={t('settings.carryRecord')} value={ask.carryRecord} onChange={(carryRecord) => changeAsk({ ...ask, carryRecord })} />
          </Group>
        </Section>

        <Section title={t('settings.display')}>
          <Group>
            <Row
              title={t('settings.theme')}
              minHeight={56}
              right={
                <Segment
                  options={themeChoices}
                  value={choice}
                  onChange={setChoice}
                  labelOf={(c) => t(`settings.theme.${c}`)}
                  look="setting"
                  label={t('settings.theme')}
                />
              }
            />
            <SwitchRow label={t('settings.aiColours')} value={aiColours.on} onChange={aiColours.setOn} />
          </Group>
        </Section>

        <Section title={t('settings.history')}>
          {kept ? (
            <Group>
              <Row title={t('settings.historyCount', { n: kept })} />
              <Row
                title={t('settings.historyClear')}
                titleStyle={s.danger}
                onPress={() =>
                  Alert.alert(t('settings.historyClearTitle'), t('settings.historyClearBody', { n: kept }), [
                    { text: t('sheet.cancel'), style: 'cancel' },
                    { text: t('drawer.remove'), style: 'destructive', onPress: () => void conversations.removeAll() },
                  ])
                }
              />
            </Group>
          ) : (
            <Text style={s.caption}>{t('settings.historyEmpty')}</Text>
          )}
        </Section>

        <Section title={t('settings.about')}>
          <Group>
            <Row
              title={`${t('app.name')} ${version}`.trim()}
              subtitle={t('app.notOfficial')}
              subtitleTone="sub"
              chevron
              minHeight={64}
              onPress={() => router.push('/about')}
            />
          </Group>
        </Section>
      </ScrollView>

      <PCSheet conn={pcOpen} onClose={() => setPCOpen(null)} onUnpaired={unpaired} />
      <DefaultsSheet open={defaultsOpen} onClose={() => setDefaultsOpen(false)} prefs={prefs} onChange={change} />
      <ModelSheet
        agent={modelOpen && moderator ? moderator.agent : null}
        tierModels={moderator?.tiers ?? {}}
        efforts={moderator?.efforts ?? {}}
        tier={ask.tier}
        value={moderatorPick}
        title={t('settings.moderatorModelTitle', { name: agentName(ask.synthAgent) })}
        ownHint={t('settings.moderatorModelHint')}
        onClose={() => setModelOpen(false)}
        onPick={(pick, close) => {
          const synthModels = { ...ask.synthModels };
          if (pick) synthModels[ask.synthAgent] = pick;
          else delete synthModels[ask.synthAgent];
          changeAsk({ ...ask, synthModels });
          if (close) setModelOpen(false);
        }}
      />
      <Sheet open={infoOpen} onClose={() => setInfoOpen(false)} title={t('settings.compareSynth')} testID="compare-info">
        <View style={s.infoBody}>
          {features.refine && <Text style={s.infoText}>{t('settings.refineHint')}</Text>}
          <Text style={s.infoText}>{t('settings.synthHint')}</Text>
          <Text style={s.infoText}>{t('settings.carryRecordHint')}</Text>
        </View>
      </Sheet>
    </View>
  );
}

function Section({ title, right, children }: { title: string; right?: ReactNode; children: ReactNode }) {
  const s = styles(useTheme());
  return (
    <View style={s.section}>
      <SectionLabel text={title} right={right} />
      {children}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { gap: 14, paddingTop: 4, paddingHorizontal: 16 },
    section: { gap: 6 },
    caption: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
    note: { paddingHorizontal: 4, fontSize: 14, lineHeight: 20.3, color: th.text },
    value: { maxWidth: 140, fontSize: 14, lineHeight: 20, color: th.textSub },
    accent: { fontWeight: '500', color: th.accent },
    danger: { fontWeight: '500', color: th.danger },
    info: { marginVertical: -11, marginRight: -6 },
    writer: { gap: 10, paddingTop: 12, paddingHorizontal: 16, paddingBottom: 14 },
    writerLabel: { fontSize: 15, lineHeight: 22.5, color: th.text },
    infoBody: { gap: 12, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    infoText: { fontSize: 15, lineHeight: 22.5, color: th.textSub },
  }),
);
