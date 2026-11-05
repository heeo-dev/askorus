// The debate under a combined answer (토론, P2-5, #26): the '토론하기'
// button with its cost (and, the first time, what the debate sends where),
// then the chat room as the PC reports it: each point with its nature chip
// and sides, the moderator's question as a small line in the middle, each
// AI's turn as a bubble with its stance chip and seconds, the point's
// outcome line, and at the end the answer after the debate in a card like
// the combined answer's. 'Stop the debate' works at any step.
//
// What to show comes from ask/debate.ts; this only draws and passes the
// presses on.

import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { DebatePoint, DebateTurn, Info } from '@/api/types';
import { plainNames } from '@/ask/aiNames';
import { debateCost, debateErrorText, debateOffered, debateView, duration, foldText, natureLabel, resultLabel, sideLine, stanceLabel } from '@/ask/debate';
import type { Turn } from '@/ask/thread';
import { t } from '@/i18n';

import { AnswerActions } from './AnswerBlock';
import { Avatar } from './Avatar';
import { Button } from './Button';
import { CombinedAnswerCard } from './CombinedAnswer';
import { Spinner } from './controls';
import { Markdown } from './Markdown';
import { themed, useTheme } from './theme';
import type { Note } from './TurnView';

export type DebateActions = {
  start: () => void;
  stop: () => void;
  poll: () => void;
  copy: (key: string, text: string) => void;
  openAIs: () => void;
  /** The person has seen what a debate sends where (asked once). */
  noticed: boolean;
  notice: () => void;
};

// A turn longer than this shows its start until 'More'.
const foldLines = 4;

export function DebateSection({ turn, info, note, act }: { turn: Turn; info: Info | null; note?: Note; act: DebateActions }) {
  const s = styles(useTheme());
  const [asking, setAsking] = useState(false);
  const view = debateView(turn);
  const offered = debateOffered(turn, info);

  const press = () => {
    if (act.noticed) act.start();
    else setAsking(true);
  };
  const button = offered ? (
    asking ? (
      <View style={s.notice} accessibilityLiveRegion="polite">
        <Text style={s.noticeTitle} accessibilityRole="header">
          {t('debate.notice.title')}
        </Text>
        <Text style={s.body}>{t('debate.notice.body')}</Text>
        <View style={s.row}>
          <Button
            label={t('debate.notice.go')}
            onPress={() => {
              setAsking(false);
              act.notice();
              act.start();
            }}
          />
          <Button variant="quiet" label={t('debate.notice.cancel')} onPress={() => setAsking(false)} />
        </View>
      </View>
    ) : (
      <View style={s.action}>
        <Button variant="outline" label={view ? t('debate.again') : t('debate.start')} onPress={press} />
        <Text style={s.caption}>{t('debate.cost', { n: debateCost(turn) })}</Text>
      </View>
    )
  ) : null;

  if (!view) return button;

  const snap = 'snap' in view ? view.snap : undefined;
  const running = view.kind === 'starting' || view.kind === 'planning' || view.kind === 'running' || view.kind === 'writing';
  const debated = snap?.points.filter((p) => p.debate).length ?? 0;
  const title = !snap?.points.length ? t('debate.start') : debated === 1 ? t('debate.titleOne') : t('debate.title', { n: debated });

  return (
    <View style={s.section}>
      <View style={s.head}>
        <Text style={s.title} accessibilityRole="header">
          {title}
        </Text>
        {running && turn.debate?.id ? <Button variant="quiet" label={t('debate.stop')} onPress={act.stop} /> : null}
      </View>

      {view.kind === 'starting' || view.kind === 'planning' ? (
        <Busy text={view.kind === 'starting' ? t('debate.starting') : t('debate.planning')} />
      ) : null}

      {snap?.points.map((p) => <Point key={p.id} p={p} />)}

      {view.kind === 'writing' ? <Busy text={t('debate.writing')} /> : null}
      {view.kind === 'none' ? <Text style={s.caption}>{t('debate.none')}</Text> : null}
      {view.kind === 'stopped' ? <Text style={s.caption}>{t('debate.stopped')}</Text> : null}
      {view.kind === 'failed' ? <Text style={s.error}>{debateErrorText(view.code, view.message)}</Text> : null}
      {view.kind === 'lost' ? (
        <View style={s.action}>
          <Text style={s.error}>{t('error.network')}</Text>
          <Button variant="outline" label={t('ask.retry')} onPress={view.action === 'start' ? act.start : act.poll} />
        </View>
      ) : null}

      {view.kind === 'done' && snap?.after ? (
        <View style={s.after}>
          <CombinedAnswerCard
            pile={{
              agents: speakers(snap.points),
              label: t('ask.answeredAIs', { n: speakers(snap.points).length, names: speakers(snap.points).map(agentName).join(', ') }),
              onPress: act.openAIs,
            }}
            title={t('debate.after')}
            meta={t('debate.meta', { time: duration(snap.ms) })}
            footnote={t('compare.synth.mayErr')}
          >
            <View style={s.afterBody}>
              <Markdown text={snap.after.text} size="synth" ais={speakers(snap.points)} />
              <AnswerActions onCopy={() => act.copy(`${turn.id}/debate`, plainNames(snap.after?.text ?? ''))} note={note} />
            </View>
          </CombinedAnswerCard>
        </View>
      ) : null}

      {button}
    </View>
  );
}

/** speakers: the AIs that spoke, in the order they first did. */
function speakers(points: DebatePoint[]): string[] {
  const out: string[] = [];
  for (const p of points) for (const x of p.turns) if (!out.includes(x.ai)) out.push(x.ai);
  for (const p of points) for (const side of p.sides) for (const a of side.ais) if (!out.includes(a)) out.push(a);
  return out;
}

function Busy({ text }: { text: string }) {
  const s = styles(useTheme());
  return (
    <View style={s.busy} accessible accessibilityLabel={text} accessibilityLiveRegion="polite">
      <Spinner size={16} />
      <Text style={s.busyText}>{text}</Text>
    </View>
  );
}

function Point({ p }: { p: DebatePoint }) {
  const s = styles(useTheme());
  const nature = natureLabel(p.nature);
  const result = resultLabel(p.result);
  return (
    <View style={s.point}>
      <View style={s.pointHead}>
        {nature ? <Pill text={nature} /> : null}
        <Text style={s.topic}>{p.topic}</Text>
      </View>
      {p.sides.map((side, i) => (
        <Text key={i} style={s.side}>
          {sideLine(side)}
        </Text>
      ))}
      {!p.debate ? <Text style={s.caption}>{t('debate.notDebated')}</Text> : null}
      {p.turns.map((x, i) => (
        <TurnBubble key={i} x={x} />
      ))}
      {p.checking ? <Busy text={t('debate.checking')} /> : null}
      {result || p.line ? (
        <View style={s.result} accessible accessibilityLabel={[result, p.line].filter(Boolean).join('. ')}>
          {result ? <Pill text={result} strong /> : null}
          {p.line ? <Text style={s.resultLine}>{plainNames(p.line)}</Text> : null}
        </View>
      ) : null}
    </View>
  );
}

function TurnBubble({ x }: { x: DebateTurn }) {
  const th = useTheme();
  const s = styles(th);
  const [open, setOpen] = useState(false);
  const name = agentName(x.ai);
  const stance = stanceLabel(x.stance);
  const secs = t('ask.seconds', { s: Math.round(x.ms / 1000) });
  const text = x.text ?? '';
  const long = text.split('\n').length > foldLines || text.length > 280;
  return (
    <View style={s.turnBox}>
      <Text style={s.ask}>
        {t('debate.ask', { name })}: {x.ask}
      </Text>
      {x.status === 'running' ? (
        <Busy text={t('debate.answering', { name, s: Math.round(x.ms / 1000) })} />
      ) : (
        <View style={s.bubble}>
          <View style={s.bubbleHead}>
            <Avatar agent={x.ai} />
            <Text style={s.name}>{name}</Text>
            {stance ? <Pill text={stance} /> : null}
            <Text style={s.secs}>{secs}</Text>
          </View>
          {x.status === 'failed' ? (
            <Text style={s.caption}>{t('debate.noReply')}</Text>
          ) : open || !long ? (
            <Markdown text={text} />
          ) : (
            <Text style={s.body} numberOfLines={foldLines}>
              {foldText(text)}
            </Text>
          )}
          {long && x.status === 'done' ? (
            <Pressable onPress={() => setOpen(!open)} accessibilityRole="button" hitSlop={8}>
              <Text style={s.more}>{open ? t('debate.less') : t('debate.more')}</Text>
            </Pressable>
          ) : null}
        </View>
      )}
    </View>
  );
}

function Pill({ text, strong }: { text: string; strong?: boolean }) {
  const s = styles(useTheme());
  return (
    <View style={[s.pill, strong && s.pillStrong]}>
      <Text style={[s.pillText, strong && s.pillTextStrong]} numberOfLines={1}>
        {text}
      </Text>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    section: { gap: 12, marginTop: 4 },
    head: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8, minHeight: 44 },
    title: { flexShrink: 1, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    action: { gap: 8, alignItems: 'flex-start' },
    row: { flexDirection: 'row', gap: 8, flexWrap: 'wrap' },
    notice: { gap: 8, padding: 12, borderRadius: 12, backgroundColor: th.soft, borderWidth: 1, borderColor: th.softLine },
    noticeTitle: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    body: { fontSize: 15, lineHeight: 22, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
    busy: { minHeight: 28, flexDirection: 'row', alignItems: 'center', gap: 10 },
    busyText: { flexShrink: 1, fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.text },
    point: { gap: 8, paddingTop: 4 },
    pointHead: { flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
    topic: { flexShrink: 1, fontSize: 15, fontWeight: '600', lineHeight: 22, color: th.text },
    side: { fontSize: 13, lineHeight: 19.5, color: th.textSub },
    turnBox: { gap: 6 },
    ask: { alignSelf: 'center', textAlign: 'center', fontSize: 12, lineHeight: 18, color: th.caption, paddingHorizontal: 12 },
    bubble: { gap: 6, padding: 12, borderRadius: 14, backgroundColor: th.raised },
    bubbleHead: { flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
    name: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    secs: { marginLeft: 'auto', fontSize: 12, lineHeight: 18, color: th.caption, fontVariant: ['tabular-nums'] },
    more: { fontSize: 13, lineHeight: 19.5, color: th.accent, fontWeight: '500' },
    pill: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 10, backgroundColor: th.chipOn },
    pillStrong: { backgroundColor: th.fill },
    pillText: { fontSize: 12, lineHeight: 16, fontWeight: '500', color: th.onChipOn },
    pillTextStrong: { color: th.onFill, fontWeight: '600' },
    result: { flexDirection: 'row', alignItems: 'flex-start', gap: 8, padding: 10, borderRadius: 12, backgroundColor: th.soft },
    resultLine: { flex: 1, fontSize: 14, lineHeight: 20, color: th.text },
    after: { marginTop: 4 },
    afterBody: { gap: 10 },
  }),
);
