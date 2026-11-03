// Settings (S5c, board F-Settings): paired PCs (a sheet each to check the
// connection or unpair), what new sessions start with, pairing another PC;
// the default ask tier; whether answers are compared on their own (P2-3)
// and a combined answer is then written and by which AI (S5b); the theme
// (system, dark or light) and whether each AI has its own colour (#6); and
// about the app.

import Constants from 'expo-constants';
import { router, useFocusEffect } from 'expo-router';
import { type ReactNode, useCallback, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { tiers } from '@/api/types';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs, synthWriters } from '@/ask/prefs';
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
import { Sheet } from '@/ui/Sheet';
import { usePCs } from '@/ui/shell/pcs';
import { themeChoices, themed, useAIColours, useTheme, useThemeChoice } from '@/ui/theme';

export default function Settings() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { list, reload } = usePCs();
  const reach = useReach(list);
  const { choice, setChoice } = useThemeChoice();
  const aiColours = useAIColours();
  const [note, setNote] = useState('');
  const [prefs, setPrefs] = useState<Prefs>({ model: '', effort: '' });
  const [ask, setAsk] = useState<AskPrefs>(defaultAskPrefs);
  const [pcOpen, setPCOpen] = useState<Connection | null>(null);
  const [defaultsOpen, setDefaultsOpen] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);

  useFocusEffect(
    useCallback(() => {
      reload().catch(() => undefined);
      loadPrefs().then(setPrefs);
      loadAskPrefs().then(setAsk);
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
            <SwitchRow label={t('settings.compareAuto')} value={ask.compare} onChange={(compare) => changeAsk({ ...ask, compare })} />
            <SwitchRow label={t('settings.synthAuto')} value={ask.synth} onChange={(synth) => changeAsk({ ...ask, synth })} />
            {/* S5b. A fixed list of writers: Settings has no PC info, and the PC falls back when it lacks the AI. */}
            <View style={s.writer}>
              <Text style={s.writerLabel}>{t('settings.synthAgentLong')}</Text>
              <PillChoice
                options={synthWriters.map((a) => ({ value: a, label: agentName(a), agent: a }))}
                value={ask.synthAgent as (typeof synthWriters)[number]}
                onChange={(synthAgent) => changeAsk({ ...ask, synthAgent })}
                label={t('settings.synthAgentLong')}
              />
            </View>
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
      <Sheet open={infoOpen} onClose={() => setInfoOpen(false)} title={t('settings.compareSynth')} testID="compare-info">
        <View style={s.infoBody}>
          <Text style={s.infoText}>{t('settings.compareHint')}</Text>
          <Text style={s.infoText}>{t('settings.synthHint')}</Text>
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
    info: { marginVertical: -11, marginRight: -6 },
    writer: { gap: 10, paddingTop: 12, paddingHorizontal: 16, paddingBottom: 14 },
    writerLabel: { fontSize: 15, lineHeight: 22.5, color: th.text },
    infoBody: { gap: 12, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    infoText: { fontSize: 15, lineHeight: 22.5, color: th.textSub },
  }),
);
