// The combined answer's block (S5c, F-Ask-Comparing and F-Result-Expanded):
// a header with the AIs that answered (a pile that opens the AI sheet),
// the title (a soft apricot chip, #36) and the times; while it is being made, its step (since C6
// only 'Writing the combined answer…') over grey lines; then the body and
// a footnote.

import type { ReactNode } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { AvatarPile } from './Avatar';
import { Skeleton, Spinner } from './controls';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';

/** A step of making the combined answer, being done now. */
export type StepView = { key: string; label: string };

export function CombinedAnswerCard({
  pile,
  title,
  meta,
  steps,
  skeleton,
  footnote,
  children,
}: {
  pile: { agents: string[]; dim?: string[]; label: string; onPress: () => void };
  title: string;
  meta?: string;
  steps?: StepView[];
  skeleton?: boolean;
  footnote?: string;
  children?: ReactNode;
}) {
  const th = useTheme();
  const s = styles(th);
  const hasSteps = !!steps?.length;
  return (
    <View style={s.card}>
      <View style={s.head} testID="combined-head">
        <Pressable
          onPress={pile.onPress}
          accessibilityRole="button"
          accessibilityLabel={pile.label}
          style={({ pressed }) => [s.pile, pressed && { backgroundColor: th.raised }]}
        >
          <AvatarPile agents={pile.agents} dim={pile.dim} ring={th.bg} />
          <Icon name="chevronDown" size={14} color={th.caption} />
        </Pressable>
        <View style={s.titleChip}>
          <Text style={s.title} numberOfLines={1} accessibilityRole="header">
            {title}
          </Text>
        </View>
        {meta ? (
          // Two lines rather than cut ('answers 11 s · combine…' on a phone,
          // 11-05): each part keeps its words together, so a line breaks only
          // after a ' · ', and two lines fit the row the pile makes tall.
          <Text style={s.meta} numberOfLines={2}>
            {metaParts(meta)}
          </Text>
        ) : null}
      </View>

      {hasSteps ? (
        <View style={s.stepsRow}>
          <View style={s.steps} accessibilityLiveRegion="polite">
            {steps?.map((st) => <Step key={st.key} step={st} />)}
          </View>
        </View>
      ) : null}

      {skeleton ? <Skeleton widths={[100, 93, 98, 61]} /> : null}

      {children}

      {footnote ? (
        <View style={s.foot}>
          <Text style={s.footText}>{footnote}</Text>
        </View>
      ) : null}
    </View>
  );
}

/** metaParts: the times with no-break spaces inside each ' · ' part, so a part never splits across lines. */
export const metaParts = (meta: string) =>
  meta
    .split(' · ')
    .map((p) => p.replace(/ /g, ' '))
    .join(' · ');

function Step({ step }: { step: StepView }) {
  const s = styles(useTheme());
  return (
    <View style={s.step} accessible accessibilityLabel={step.label}>
      <Spinner size={16} />
      <Text style={s.stepText}>{step.label}</Text>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    card: { gap: 8 },
    head: { flexDirection: 'row', alignItems: 'center', gap: 8, marginVertical: -10 },
    pile: { height: 44, flexDirection: 'row', alignItems: 'center', gap: 4, marginLeft: -4, paddingHorizontal: 4, borderRadius: 22 },
    // The title keeps its room; the times give way, onto a second line (English 'answers 23 s · combined in 30 s' is long).
    // #36 (11-09, the person found '종합 답변' and '토론 뒤 정리' hard to spot): a soft apricot chip, apricot words in dark.
    titleChip: { flexShrink: 0, maxWidth: '60%', paddingHorizontal: 10, paddingVertical: 3, borderRadius: 12, backgroundColor: th.soft },
    title: { fontSize: 14, fontWeight: '700', lineHeight: 20, color: th.accent },
    meta: { marginLeft: 'auto', flexShrink: 1, minWidth: 0, textAlign: 'right', fontSize: 13, lineHeight: 18, color: th.caption, fontVariant: ['tabular-nums'] },
    stepsRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 4 },
    steps: { flex: 1, minWidth: 0, gap: 2 },
    step: { minHeight: 28, flexDirection: 'row', alignItems: 'center', gap: 10, flexWrap: 'wrap' },
    stepText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.text },
    foot: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
    footText: { flex: 1, fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
