// One question and what came of it, in the 한 번에 conversation (S5c). The
// question is a bubble at the right. A question compared across the AIs
// then shows, while they answer, who is answering (F-Ask-Answering); once
// the answers are in, the combined answer's block (its steps while the AIs
// compare and one writes, then the answer with where the AIs differ, or
// why there is none), the AIs' own answers as cards, and the evidence:
// four counts, or its four sections (F-Ask-Comparing, F-Result-Expanded,
// F-Result-Evidence). Any other question (to one AI, to two, or on a PC
// whose AIs do not judge) shows each AI's answer in full.
//
// All of it comes from compare.ts and present.ts; this only draws and
// passes the presses on to the Ask screen, which keeps the state.

import { StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Info, RunPart } from '@/api/types';
import { plainNames } from '@/ask/aiNames';
import {
  answered,
  type CardKind,
  cards,
  compareErrorText,
  compareState,
  evidenceFolded,
  failedWith,
  longAnswers,
  synthErrorText,
  synthState,
} from '@/ask/compare';
import {
  answeredAgents,
  blockTitle,
  cardPreview,
  differHeadline,
  differRows,
  evidenceSections,
  metaLine,
  type PartView,
  partViews,
  snapshotOf,
  steps,
  synthExpected,
  tiles,
} from '@/ask/present';
import type { Turn } from '@/ask/thread';
import { t } from '@/i18n';

import { AICardStrip } from './AICardStrip';
import { AnswerActions, AnswerBlock } from './AnswerBlock';
import { AnsweringList } from './AnsweringList';
import { Button } from './Button';
import { CombinedAnswerCard } from './CombinedAnswer';
import { DisagreementHighlight } from './DisagreementHighlight';
import { EvidenceCounts, EvidenceSections } from './Evidence';
import { Markdown } from './Markdown';
import { themed, useTheme } from './theme';
import { ThreadBubble } from './ThreadBubble';

/** What the Ask screen keeps of a turn's view: evidence opened or folded (undefined: as the state says), the sections shown in full, the differ lines opened. */
export type TurnUI = { evidence?: boolean; more: CardKind[]; expanded: string[] };

export type Note = { text: string; error?: boolean };

/** What a turn's presses ask the Ask screen to do. */
export type TurnActions = {
  compare: (how: 'start' | 'retry' | 'poll' | 'synth') => void;
  retryRun: () => void;
  openAnswer: (agent: string) => void;
  openPair: (key: string) => void;
  openJudges: () => void;
  openAIs: () => void;
  copy: (key: string, text: string) => void;
  /** Follow up with this AI only; null when it cannot answer now. */
  only: (agent: string) => (() => void) | undefined;
  /** Carry Claude's ask on in the Claude app; null when it cannot. */
  carryOn: (key: string, p: RunPart) => (() => void) | undefined;
  setEvidence: (open: boolean) => void;
  more: (kind: CardKind) => void;
  toggleDiffer: (key: string) => void;
};

export function TurnView({
  turn,
  info,
  now,
  only,
  notes,
  ui,
  act,
}: {
  turn: Turn;
  info: Info | null;
  /** The phone's clock when drawn (the polls redraw it). */
  now: number;
  /** The AI the next follow-up goes to alone (shown on its card), or null. */
  only: string | null;
  notes: Record<string, Note>;
  ui: TurnUI;
  act: TurnActions;
}) {
  const s = styles(useTheme());
  const st = compareState(turn);
  const views = partViews(turn, now, info);
  const compared = turn.compare?.start === 'auto' || turn.compare?.start === 'manual';
  const lost = turn.error ? (
    <View style={s.lost}>
      <Text style={s.error}>{turn.error}</Text>
      <Button variant="outline" label={t('ask.retry')} onPress={act.retryRun} />
    </View>
  ) : null;

  // Not compared: each AI's answer in full (P2-2), and why there is no comparison.
  if (!compared || !st) {
    return (
      <View style={s.turn}>
        <ThreadBubble text={turn.question} />
        {st?.kind === 'noJudge' ? <Text style={s.caption}>{t(`compare.noJudge.${st.why}`)}</Text> : null}
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
    const caption =
      going && turn.compare?.start === 'auto'
        ? turn.compare.synth && !turn.compare.synth.off
          ? t('ask.willCompare')
          : t('ask.willCompareOnly')
        : undefined;
    return (
      <View style={s.turn}>
        <ThreadBubble text={turn.question} />
        {turn.error && !turn.run ? null : <AnsweringList rows={views} heading={going ? t('ask.answering') : undefined} caption={caption} />}
        {lost}
      </View>
    );
  }

  return (
    <View style={s.turn}>
      <ThreadBubble text={turn.question} />
      <Result turn={turn} info={info} views={views} only={only} notes={notes} ui={ui} act={act} />
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
  ui,
  act,
}: {
  turn: Turn;
  info: Info | null;
  views: PartView[];
  only: string | null;
  notes: Record<string, Note>;
  ui: TurnUI;
  act: TurnActions;
}) {
  const s = styles(useTheme());
  const st = compareState(turn);
  if (!st) return null;
  const snap = snapshotOf(turn);
  const synth = synthState(turn, info);
  const stepList = steps(turn, info);
  const ended = st.kind === 'done';
  const all = snap ? cards(snap) : null;
  const cs = all && (st.kind === 'comparing' || ended) ? all : null;
  const folded = ui.evidence === undefined ? evidenceFolded(synth) : !ui.evidence;
  const answeredList = answeredAgents(turn);
  const judgingInfo = !!snap || st.kind === 'manual' || st.kind === 'comparing' || st.kind === 'done' || st.kind === 'failed';
  const infoButton = judgingInfo ? { label: t('compare.info'), onPress: act.openJudges } : undefined;

  // Where the comparison stands when it says something besides its steps.
  const status = (() => {
    switch (st.kind) {
      case 'manual':
        return (
          <View style={s.action}>
            <Button variant="outline" label={t('compare.start')} onPress={() => act.compare('start')} />
            {st.cost > 0 ? <Text style={s.caption}>{t('compare.startCost', { n: st.cost })}</Text> : null}
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
      case 'done': {
        const differ = all?.find((c) => c.kind === 'differ')?.items ?? [];
        return (
          <View style={s.synth}>
            <Markdown text={synth.text} size="synth" ais={answeredList} />
            {differ.map((item) => {
              const head = differHeadline(item);
              return (
                <DisagreementHighlight
                  key={item.key}
                  lead={head.summary ? '' : t('compare.differLead')}
                  headline={head.text}
                  groupLabel={t('compare.differGroup')}
                  expanded={ui.expanded.includes(item.key)}
                  onToggle={() => act.toggleDiffer(item.key)}
                  rows={differRows(snap, item).map((r) => ({ ...r, onPress: () => act.openAnswer(r.agent) }))}
                />
              );
            })}
            {!synth.judged ? <Text style={s.caption}>{t('compare.synth.noJudge')}</Text> : null}
            {synth.unnamed > 0 ? <Text style={s.warn}>{t('compare.synth.unnamed')}</Text> : null}
            <AnswerActions onCopy={() => act.copy(`${turn.id}/synth`, plainNames(synth.text))} note={notes[`${turn.id}/synth`]} />
          </View>
        );
      }
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
        info={infoButton}
        skeleton={stepList.length > 0 && synthExpected(turn)}
        footnote={synth?.kind === 'done' ? t('compare.synth.mayErr') : undefined}
      >
        {status}
        {body}
        {ended && st.retry ? <Button variant="outline" label={t('compare.retry')} onPress={() => act.compare('retry')} /> : null}
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

      {cs && folded ? <EvidenceCounts title={t('compare.evidence')} tiles={tiles(cs, ended)} onOpen={() => act.setEvidence(true)} /> : null}
      {cs && !folded ? (
        <EvidenceSections
          title={t('compare.evidence')}
          collapseLabel={t('compare.evidence.collapse')}
          onCollapse={() => act.setEvidence(false)}
          info={infoButton}
          sections={evidenceSections({
            cards: cs,
            ended,
            partial: ended && st.partial,
            failed: snap ? failedWith(snap) : [],
            more: ui.more,
            onMore: act.more,
            onPair: act.openPair,
          })}
        />
      ) : null}
      {cs && !folded && snap
        ? longAnswers(snap).map((l) => (
            <Text key={l.agent} style={s.caption}>
              {t('compare.long', { name: l.agent, n: l.judged })}
            </Text>
          ))
        : null}
    </>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    turn: { gap: 10 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
    // A note to act on: in the text colour, so it does not read as fine print.
    warn: { fontSize: 13, fontWeight: '600', lineHeight: 19.5, color: th.text },
    lost: { gap: 8 },
    action: { gap: 8, alignItems: 'flex-start' },
    synth: { gap: 10 },
    cards: { marginTop: 8 },
  }),
);
