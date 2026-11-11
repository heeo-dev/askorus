// An example conversation (#51, '/example'), opened from the first screen
// while no PC is paired: one question, three AIs' answers, the moderator's
// combined answer and a debate, drawn by the same parts as a real
// conversation (ui/TurnView). The answers are real ones kept with the app
// (ask/example); a line on top says so, always in view.
//
// What needs a PC is not here: no follow-up, no 'only this AI', no carrying
// on in the Claude app, no new debate. Opening an AI's answer, Copy, Share
// and Report work as anywhere (#50: Report opens the mail app). Nothing is
// asked of any PC and nothing is kept in the history. Where the follow-up
// bar would be, the screen offers pairing.

import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useMemo, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { exampleDate, exampleTurns } from '@/ask/example';
import { aiStatus, metaLine, partViews, rowValue } from '@/ask/present';
import type { Turn } from '@/ask/thread';
import { currentLang, t } from '@/i18n';
import { AISheet } from '@/ui/AISheet';
import { answerBy } from '@/ui/AnswerBlock';
import { AnswerSheet } from '@/ui/AnswerSheet';
import { Button } from '@/ui/Button';
import { TopBar } from '@/ui/Header';
import { useSafeInsets } from '@/ui/keyboard';
import { themed, useTheme } from '@/ui/theme';
import { type Note, type TurnActions, TurnView } from '@/ui/TurnView';

type SheetState = { kind: 'ais'; turn: string } | { kind: 'answer'; turn: string; agent: string } | null;

const nothing = () => undefined;

export default function Example() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const lang = currentLang();
  const turns = useMemo(() => exampleTurns(lang), [lang]);
  const [notes, setNotes] = useState<Record<string, Note>>({});
  const [sheet, setSheet] = useState<SheetState>(null);
  // Opened from a link with nothing behind it, Back leads to the app's start.
  const back = () => (router.canGoBack() ? router.back() : router.replace('/'));

  const copy = async (key: string, text: string) => {
    await Clipboard.setStringAsync(text);
    setNotes((m) => ({ ...m, [key]: { text: t('sheet.copied') } }));
  };

  // The presses a turn passes on: what needs a PC does nothing, and the
  // turn does not offer it (no PC's info, a debate already held).
  const actsFor = (turn: Turn): TurnActions => ({
    compare: nothing,
    retryRun: nothing,
    openAnswer: (agent) => setSheet({ kind: 'answer', turn: turn.id, agent }),
    openAIs: () => setSheet({ kind: 'ais', turn: turn.id }),
    openRefine: nothing,
    openRecord: nothing,
    copy,
    only: () => undefined,
    carryOn: () => undefined,
    debate: { start: nothing, stop: nothing, poll: nothing, copy, openAIs: () => setSheet({ kind: 'ais', turn: turn.id }), noticed: true, notice: nothing },
  });

  const sheetTurn = sheet ? (turns.find((x) => x.id === sheet.turn) ?? null) : null;
  // The clock stands at the moment the turn was asked: nothing here is still coming.
  const views = sheetTurn ? partViews(sheetTurn, sheetTurn.at ?? 0, null) : [];
  const answerView = sheet?.kind === 'answer' ? (views.find((p) => p.agent === sheet.agent) ?? null) : null;
  const answerKey = answerView && sheetTurn ? `${sheetTurn.id}/${answerView.agent}` : '';

  return (
    <View style={s.screen}>
      <TopBar title={t('example.title')} onBack={back} />
      <View style={s.banner}>
        <Text style={s.bannerText}>{t('example.banner', { date: exampleDate[lang] })}</Text>
      </View>
      <ScrollView style={s.fill} contentContainerStyle={s.thread}>
        {turns.map((turn) => (
          <TurnView key={turn.id} turn={turn} info={null} now={turn.at ?? 0} only={null} notes={notes} act={actsFor(turn)} />
        ))}
      </ScrollView>
      <View style={[s.foot, { paddingBottom: 12 + insets.bottom }]}>
        <Text style={s.footText}>{t('example.pairHint')}</Text>
        <Button label={t('home.pair')} onPress={() => router.push('/pair')} />
      </View>
      <AISheet
        open={sheet?.kind === 'ais' && !!sheetTurn}
        onClose={() => setSheet(null)}
        title={t('ask.aiCount', { n: views.length })}
        subtitle={(sheetTurn && metaLine(sheetTurn)) || undefined}
        rows={views.map((p) => ({
          agent: p.agent,
          name: p.name,
          model: p.model || undefined,
          status: aiStatus(p),
          onPress: () => setSheet({ kind: 'answer', turn: sheetTurn?.id ?? '', agent: p.agent }),
          accessibilityValue: rowValue(p),
        }))}
      />
      <AnswerSheet
        p={answerView}
        onClose={() => setSheet(null)}
        onCopy={answerView ? () => copy(answerKey, answerView.part.answer ?? '') : undefined}
        more={answerView ? { ai: answerBy(answerView), text: answerView.part.answer ?? '', question: sheetTurn?.question } : undefined}
        note={answerKey ? notes[answerKey] : undefined}
      />
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    fill: { flex: 1 },
    banner: { marginHorizontal: 16, marginBottom: 8, paddingVertical: 10, paddingHorizontal: 14, borderRadius: 12, backgroundColor: th.raised },
    bannerText: { fontSize: 13, lineHeight: 19.5, color: th.textSub },
    thread: { gap: 28, paddingHorizontal: 20, paddingTop: 4, paddingBottom: 20 },
    foot: { gap: 10, paddingTop: 12, paddingHorizontal: 16, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: th.border, backgroundColor: th.bg },
    footText: { fontSize: 13, lineHeight: 19.5, color: th.caption, textAlign: 'center' },
  }),
);
