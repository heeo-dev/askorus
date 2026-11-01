// The 정리 (Summary) tab of a compared turn (P2-3): where the comparison
// stands, the chips and cards (Differ first; the other cards once every
// judge ended), and a footer saying who judged and what that means. All of
// it comes from compare.ts; this only draws and passes the presses on.

import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { Info } from '@/api/types';
import {
  answered,
  answeringLines,
  type CardKind,
  cardKinds,
  type CardView,
  cards,
  chipCounts,
  compareErrorText,
  compareState,
  failedWith,
  firstShown,
  judgeModels,
  longAnswers,
  shownCards,
} from '@/ask/compare';
import type { Turn } from '@/ask/thread';
import { t } from '@/i18n';

import { Button } from './Button';
import { CompareItem } from './CompareItem';
import { type Theme, useTheme } from './theme';

export function CompareSection({
  turn,
  info,
  onAction,
  onPair,
  onJudges,
}: {
  turn: Turn;
  info: Info | null;
  onAction: (how: 'start' | 'retry' | 'poll') => void;
  onPair: (itemKey: string) => void;
  onJudges: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const [only, setOnly] = useState<CardKind | null>(null);
  const [open, setOpen] = useState<Set<CardKind>>(() => new Set());
  const state = compareState(turn);
  if (!state) return null;

  const c = turn.compare;
  const snap = c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
  const comparing = state.kind === 'comparing';
  const ended = state.kind === 'done';
  const cs = snap && (comparing || ended) ? cards(snap) : null;

  const status = (() => {
    switch (state.kind) {
      case 'manual':
        return (
          <>
            <Button label={t('compare.start')} onPress={() => onAction('start')} />
            {state.cost > 0 ? <Text style={s.muted}>{t('compare.startCost', { n: state.cost })}</Text> : null}
          </>
        );
      case 'noJudge':
        return <Text style={s.muted}>{t(`compare.noJudge.${state.why}`)}</Text>;
      case 'oneAnswer':
        // 'Only one AI answered' only when one did; with none (or answers the PC could not use) the plain rule.
        return <Text style={s.muted}>{answered(turn).n === 1 ? t('compare.oneAnswer') : compareErrorText('need_two_answers')}</Text>;
      case 'comparing': {
        const typical = info?.judging?.verified && info.judging.typicalMs ? Math.round(info.judging.typicalMs / 1000) : 0;
        return (
          <>
            <Text style={s.muted}>
              {state.total ? t('compare.comparing', { done: state.ended, total: state.total }) : t('compare.comparingStart')}
            </Text>
            {typical ? <Text style={s.muted}>{t('compare.typical', { s: typical })}</Text> : null}
          </>
        );
      }
      case 'failed':
        return (
          <>
            <Text style={s.error}>{compareErrorText(state.code, state.message)}</Text>
            {state.action === 'retry' ? <Button secondary label={t('compare.retry')} onPress={() => onAction('retry')} /> : null}
            {state.action === 'start' ? <Button secondary label={t('compare.start')} onPress={() => onAction('start')} /> : null}
          </>
        );
      case 'notFound':
        return (
          <>
            <Text style={s.muted}>{compareErrorText('comparison_not_found')}</Text>
            <Button secondary label={t('compare.retry')} onPress={() => onAction('start')} />
          </>
        );
      case 'lost': {
        const action = state.action;
        return (
          <>
            <Text style={s.error}>{t('error.network')}</Text>
            <Button secondary label={t('ask.retry')} onPress={() => onAction(action)} />
          </>
        );
      }
      default:
        return null;
    }
  })();

  const list = judgeModels(turn, info)
    .map((j) => (j.model ? `${j.agent} ${j.model}` : j.agent))
    .join(' · ');
  const footer =
    snap || ['manual', 'comparing', 'done', 'failed'].includes(state.kind) ? (
      <View style={s.footer}>
        {/* No link when no pair had a judge (no_judge): there is nobody to name. */}
        {snap && list ? (
          <Pressable onPress={onJudges} accessibilityRole="button" hitSlop={6} style={({ pressed }) => pressed && s.pressed}>
            <Text style={s.link}>{t('compare.footer.judges', { list })} ›</Text>
          </Pressable>
        ) : list ? (
          <Text style={s.small}>{t('compare.footer.judges', { list })}</Text>
        ) : null}
        <Text style={s.small}>{t('compare.footer.company')}</Text>
        <Text style={s.small}>{t('compare.footer.mayErr')}</Text>
        {!info?.judging?.verified ? <Text style={s.small}>{t('compare.footer.unverified')}</Text> : null}
      </View>
    ) : null;

  return (
    <View style={s.root}>
      {answeringLines(turn, Date.now()).map((l) => (
        <Text key={l.agent} style={s.muted}>
          {t('compare.answering', { name: l.agent, s: l.s })}
        </Text>
      ))}
      {status}
      {ended && state.retry ? <Button secondary label={t('compare.retry')} onPress={() => onAction('retry')} /> : null}
      {cs && snap ? (
        <Cards
          th={th}
          cs={cs}
          ended={ended}
          partial={ended && state.partial}
          failed={failedWith(snap)}
          only={only}
          onOnly={setOnly}
          open={open}
          onOpen={(kind) => setOpen((o) => new Set(o).add(kind))}
          onPair={onPair}
        />
      ) : null}
      {cs && snap
        ? longAnswers(snap).map((l) => (
            <Text key={l.agent} style={s.muted}>
              {t('compare.long', { name: l.agent, n: l.judged })}
            </Text>
          ))
        : null}
      {footer}
    </View>
  );
}

function Cards({
  th,
  cs,
  ended,
  partial,
  failed,
  only,
  onOnly,
  open,
  onOpen,
  onPair,
}: {
  th: Theme;
  cs: CardView[];
  ended: boolean;
  partial: boolean;
  failed: string[];
  only: CardKind | null;
  onOnly: (k: CardKind | null) => void;
  open: Set<CardKind>;
  onOpen: (k: CardKind) => void;
  onPair: (itemKey: string) => void;
}) {
  const s = styles(th);
  const counts = chipCounts(cs);
  const none = ended && cs.every((x) => x.items.length === 0);
  // While judges run only Differ can fill; an empty card says nothing yet.
  const shown = shownCards(cs, { only, ended }).filter((x) => ended || only || x.items.length > 0);
  return (
    <>
      <View style={s.chips}>
        {cardKinds.map((kind) => {
          const label = `${t(`compare.kind.${kind}`)} ${!ended && kind !== 'differ' ? '…' : counts[kind]}`;
          const on = only === kind;
          return (
            <Pressable
              key={kind}
              onPress={() => onOnly(on ? null : kind)}
              style={({ pressed }) => [s.chip, on && s.chipOn, pressed && s.pressed]}
              accessibilityRole="button"
              accessibilityLabel={label}
              accessibilityState={{ selected: on }}
            >
              <Text style={[s.chipText, on && s.chipTextOn]}>{label}</Text>
            </Pressable>
          );
        })}
      </View>
      {none ? (
        <Text style={s.muted}>{t('compare.noCore')}</Text>
      ) : (
        shown.map((card) => {
          const all = open.has(card.kind);
          const items = all ? card.items : card.items.slice(0, firstShown);
          const rest = card.items.length - items.length;
          return (
            <View key={card.kind} style={s.card}>
              <Text style={s.cardTitle} accessibilityRole="header">
                {t(`compare.kind.${card.kind}`)}
              </Text>
              <Text style={s.meaning}>{t(`compare.meaning.${card.kind}`)}</Text>
              {items.map((it) => (
                <CompareItem key={it.key} item={it} onPair={() => onPair(it.key)} />
              ))}
              {/* A picked card with nothing yet while judges run: it may still fill. */}
              {!card.items.length && !ended ? <Text style={s.muted}>{t('compare.comparingStart')}</Text> : null}
              {card.kind === 'differ' && !card.items.length && ended ? (
                <Text style={s.muted}>
                  {partial
                    ? [t('compare.noDifferPartial'), failed.length ? t('compare.cantCompare', { names: failed.join(', ') }) : '']
                        .filter(Boolean)
                        .join(' ')
                    : t('compare.noDiffer')}
                </Text>
              ) : null}
              {rest > 0 ? <Button secondary label={t('compare.more', { n: rest })} onPress={() => onOpen(card.kind)} /> : null}
            </View>
          );
        })
      )}
    </>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    root: { gap: 10 },
    muted: { color: th.muted, fontSize: 14, lineHeight: 20 },
    error: { color: th.danger, fontSize: 14, lineHeight: 20 },
    small: { color: th.muted, fontSize: 12, lineHeight: 17 },
    link: { color: th.accent, fontSize: 13, lineHeight: 18, fontWeight: '600' },
    footer: { gap: 4, paddingTop: 4 },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    chip: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 12,
      paddingVertical: 6,
      backgroundColor: th.card,
    },
    chipOn: { backgroundColor: th.accent, borderColor: th.accent },
    chipText: { color: th.text, fontSize: 14 },
    chipTextOn: { color: th.accentText, fontWeight: '600' },
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 14,
      gap: 8,
    },
    cardTitle: { color: th.text, fontSize: 16, fontWeight: '700' },
    meaning: { color: th.muted, fontSize: 13, lineHeight: 18 },
    pressed: { opacity: 0.6 },
  });
