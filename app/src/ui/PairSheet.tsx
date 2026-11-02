// The sheet of one evidence item (P2-3, S5c), opened from a Differ or Check
// this item: each side's AIs and sentences (the numbers that differ in
// bold, each AI's name over its sentence when a side has more than one),
// why the item needs a look, the judgments of each direction as one line
// per direction (R-41), and a way to each AI's whole answer. No
// probabilities: who judged is what the person can weigh.

import { StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Comparison } from '@/api/types';
import { pairView, whyText } from '@/ask/compare';
import { groupJudgments } from '@/ask/present';
import { t } from '@/i18n';

import { Button } from './Button';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';
import { UnitText } from './UnitText';

export function PairSheet({
  snap,
  itemKey,
  onClose,
  onShowAnswer,
}: {
  snap?: Comparison;
  itemKey: string | null;
  onClose: () => void;
  /** Show this AI's answer (an agent code). */
  onShowAnswer: (agent: string) => void;
}) {
  const s = styles(useTheme());
  const v = snap && itemKey ? pairView(snap, itemKey) : null;
  if (!v) return null;
  const why = v.why.map(whyText).filter((x): x is string => !!x);
  const lines = groupJudgments(v.lines);
  return (
    <Sheet open onClose={onClose} title={t(`compare.kind.${v.item.kind}`)} testID="pair-sheet">
      <View style={s.body}>
        {v.item.sides.map((side, i) => (
          <View key={i} style={s.side}>
            <Text style={s.who}>{side.agents.join(', ')}</Text>
            {side.units.map((u) => (
              <View key={u.id} style={s.unit}>
                {side.agents.length > 1 ? <Text style={s.unitWho}>{u.agent}</Text> : null}
                <UnitText unit={u} spans={side.numberSpans} />
              </View>
            ))}
          </View>
        ))}
        {why.length ? (
          <View style={s.section}>
            <Text style={s.heading} accessibilityRole="header">
              {t('compare.whyTitle')}
            </Text>
            {why.map((w) => (
              <Text key={w} style={s.line}>
                {w}
              </Text>
            ))}
          </View>
        ) : null}
        {lines.length ? (
          <View style={s.section}>
            <Text style={s.heading} accessibilityRole="header">
              {t('compare.judgments')}
            </Text>
            {lines.map((l) => (
              <Text key={l.key} style={s.line}>
                {l.text}
              </Text>
            ))}
          </View>
        ) : null}
        <View style={s.links}>
          {v.agents.map((a) => (
            <Button key={a} variant="outline" label={t('compare.viewAnswer', { name: agentName(a) })} onPress={() => onShowAnswer(a)} />
          ))}
        </View>
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 14, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    side: { gap: 8, padding: 12, borderRadius: 12, backgroundColor: th.raised },
    who: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    unit: { gap: 2 },
    unitWho: { fontSize: 12, lineHeight: 17, color: th.caption },
    section: { gap: 6 },
    heading: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    line: { fontSize: 13.5, lineHeight: 20.25, color: th.textSub },
    links: { gap: 8 },
  }),
);
