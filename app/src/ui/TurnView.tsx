// One question and what came of it, in the 한 번에 conversation (S5c). The
// question is a bubble at the right. A question compared across the AIs
// then shows, while they answer, who is answering (F-Ask-Answering); once
// the answers are in, the combined answer's block (its step while the
// moderator writes, then the answer, or why there is none and how to get
// one) and the AIs' own answers as cards (F-Ask-Comparing,
// F-Result-Expanded). Any other question (to one AI, or on a PC whose AIs
// do not write) shows each AI's answer in full.
//
// All of it comes from compare.ts and present.ts; this only draws and
// passes the presses on to the Ask screen, which keeps the state.

import { StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Info, RunPart } from '@/api/types';
import { plainNames } from '@/ask/aiNames';
import { answered, compareErrorText, compareState, synthErrorText, synthState } from '@/ask/compare';
import { answeredAgents, blockTitle, cardPreview, metaLine, type PartView, partViews, steps, synthExpected } from '@/ask/present';
import type { Turn } from '@/ask/thread';
import { t } from '@/i18n';

import { AICardStrip } from './AICardStrip';
import { AnswerActions, AnswerBlock } from './AnswerBlock';
import { AnsweringList } from './AnsweringList';
import { Button } from './Button';
import { CombinedAnswerCard } from './CombinedAnswer';
import { Markdown } from './Markdown';
import { themed, useTheme } from './theme';
import { RefineLine } from './Refine';
import { ThreadBubble } from './ThreadBubble';

export type Note = { text: string; error?: boolean };

/** What a turn's presses ask the Ask screen to do. */
export type TurnActions = {
  compare: (how: 'start' | 'retry' | 'poll' | 'synth') => void;
  retryRun: () => void;
  openAnswer: (agent: string) => void;
  openAIs: () => void;
  /** Show what refining sent with the question (milestone C). */
  openRefine: () => void;
  copy: (key: string, text: string) => void;
  /** Follow up with this AI only; null when it cannot answer now. */
  only: (agent: string) => (() => void) | undefined;
  /** Carry Claude's ask on in the Claude app; null when it cannot. */
  carryOn: (key: string, p: RunPart) => (() => void) | undefined;
};

export function TurnView({
  turn,
  info,
  now,
  only,
  notes,
  act,
}: {
  turn: Turn;
  info: Info | null;
  /** The phone's clock when drawn (the polls redraw it). */
  now: number;
  /** The AI the next follow-up goes to alone (shown on its card), or null. */
  only: string | null;
  notes: Record<string, Note>;
  act: TurnActions;
}) {
  const s = styles(useTheme());
  const st = compareState(turn);
  const views = partViews(turn, now, info);
  const lost = turn.error ? (
    <View style={s.lost}>
      <Text style={s.error}>{turn.error}</Text>
      <Button variant="outline" label={t('ask.retry')} onPress={act.retryRun} />
    </View>
  ) : null;

  // Not compared: each AI's answer in full (P2-2).
  if (!st) {
    return (
      <View style={s.turn}>
        <ThreadBubble text={turn.question} />
        <RefineLine refine={turn.run?.refine} onOpen={act.openRefine} />
        {turn.error && !turn.run
          ? null
          : views.map((p) => (
              <AnswerBlock
                key={p.agent}
                p={p}
                onCopy={() => act.copy(`${turn.id}/${p.agent}`, p.part.answer ?? '')}
                onOnly={act.only(p.agent)}
                onContinue={act.carryOn(`${turn.id}/${p.agent}`, p.part)}
                note={notes[`${turn.id}/${p.agent}`]}
              />
            ))}
        {lost}
      </View>
    );
  }

  // Compared, still answering: who is at it, and what comes once all are in.
  if (st.kind === 'answering') {
    const going = !turn.error;
    const caption = going && turn.compare?.start === 'auto' && turn.compare.synth && !turn.compare.synth.off ? t('ask.willSynth') : undefined;
    return (
      <View style={s.turn}>
        <ThreadBubble text={turn.question} />
        <RefineLine refine={turn.run?.refine} onOpen={act.openRefine} />
        {turn.error && !turn.run ? null : <AnsweringList rows={views} heading={going ? t('ask.answering') : undefined} caption={caption} />}
        {lost}
      </View>
    );
  }

  return (
    <View style={s.turn}>
      <ThreadBubble text={turn.question} />
      <RefineLine refine={turn.run?.refine} onOpen={act.openRefine} />
      <Result turn={turn} info={info} views={views} only={only} notes={notes} act={act} />
      {lost}
    </View>
  );
}

function Result({
  turn,
  info,
  views,
  only,
  notes,
  act,
}: {
  turn: Turn;
  info: Info | null;
  views: PartView[];
  only: string | null;
  notes: Record<string, Note>;
  act: TurnActions;
}) {
  const s = styles(useTheme());
  const st = compareState(turn);
  if (!st) return null;
  const synth = synthState(turn, info);
  const stepList = steps(turn, info);
  const answeredList = answeredAgents(turn);

  // Where the comparison stands when it says something besides its step.
  const status = (() => {
    switch (st.kind) {
      case 'manual':
        return (
          <View style={s.action}>
            <Button variant="outline" label={t('compare.start')} onPress={() => act.compare('start')} />
          </View>
        );
      case 'oneAnswer':
        // 'Only one AI answered' only when one did; with none (or answers the PC could not use) the plain rule.
        return <Text style={s.caption}>{answered(turn).n === 1 ? t('compare.oneAnswer') : compareErrorText('need_two_answers')}</Text>;
      case 'failed':
        return (
          <View style={s.action}>
            <Text style={s.error}>{compareErrorText(st.code, st.message)}</Text>
            {st.action === 'retry' ? <Button variant="outline" label={t('compare.retry')} onPress={() => act.compare('retry')} /> : null}
            {st.action === 'start' ? <Button variant="outline" label={t('compare.start')} onPress={() => act.compare('start')} /> : null}
          </View>
        );
      case 'notFound':
        return (
          <View style={s.action}>
            <Text style={s.caption}>{compareErrorText('comparison_not_found')}</Text>
            <Button variant="outline" label={t('compare.retry')} onPress={() => act.compare('start')} />
          </View>
        );
      case 'lost': {
        const how = st.action;
        return (
          <View style={s.action}>
            <Text style={s.error}>{t('error.network')}</Text>
            <Button variant="outline" label={t('ask.retry')} onPress={() => act.compare(how)} />
          </View>
        );
      }
      default:
        return null;
    }
  })();

  // The combined answer itself, or why there is none and how to get one.
  const body = (() => {
    if (!synth) return null;
    switch (synth.kind) {
      case 'off':
        return (
          <View style={s.action}>
            <Button variant="outline" label={t('compare.synth.write')} onPress={() => act.compare('synth')} />
            <Text style={s.caption}>{t('compare.synth.cost', { n: synth.cost })}</Text>
          </View>
        );
      case 'failed':
        return (
          <View style={s.action}>
            <Text style={s.error}>{synthErrorText(synth.code, synth.message)}</Text>
            {synth.retry ? <Button variant="outline" label={t('compare.synth.rewrite')} onPress={() => act.compare('synth')} /> : null}
          </View>
        );
      case 'done':
        return (
          <View style={s.synth}>
            <Markdown text={synth.text} size="synth" ais={answeredList} />
            <AnswerActions onCopy={() => act.copy(`${turn.id}/synth`, plainNames(synth.text))} note={notes[`${turn.id}/synth`]} />
          </View>
        );
      default:
        return null;
    }
  })();

  return (
    <>
      <CombinedAnswerCard
        pile={{
          agents: answeredList,
          label: t('ask.answeredAIs', { n: answeredList.length, names: answeredList.map(agentName).join(', ') }),
          onPress: act.openAIs,
        }}
        title={blockTitle(turn, info)}
        meta={metaLine(turn) || undefined}
        steps={stepList}
        skeleton={stepList.length > 0 && synthExpected(turn)}
        footnote={synth?.kind === 'done' ? t('compare.synth.mayErr') : undefined}
      >
        {status}
        {body}
      </CombinedAnswerCard>

      {/* Under the grey lines the cards keep a little more room (F-Ask-Comparing). */}
      <View style={stepList.length ? s.cards : undefined}>
        <AICardStrip
          title={t('ask.byAI')}
          count={t('ask.countN', { n: views.length })}
          cards={views.map((p) => {
            const preview = cardPreview(p);
            const selected = only === p.agent;
            return {
              key: p.agent,
              agent: p.agent,
              name: p.name,
              model: p.model || undefined,
              preview,
              state: p.state,
              selected,
              selectedLabel: t('ask.onlyThis'),
              a11yLabel: [t('ask.cardLabel', { name: p.name }), selected ? t('ask.onlyThis') : p.model, preview].filter(Boolean).join('. '),
              onPress: () => act.openAnswer(p.agent),
            };
          })}
        />
      </View>
    </>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    turn: { gap: 10 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
    lost: { gap: 8 },
    action: { gap: 8, alignItems: 'flex-start' },
    synth: { gap: 10 },
    cards: { marginTop: 8 },
  }),
);
