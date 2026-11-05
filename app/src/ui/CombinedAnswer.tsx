// The combined answer's block (S5c, F-Ask-Comparing and F-Result-Expanded):
// a header with the AIs that answered (a pile that opens the AI sheet),
// the title, the times and the judging details button (always at the
// header's end, #3: at the foot after the steps it went unnoticed); while
// it is being made, its steps (comparing, then writing) over grey lines;
// then the body and a footnote.

import type { ReactNode } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';

import { AvatarPile } from './Avatar';
import { Skeleton, Spinner } from './controls';
import { Icon, IconButton } from './Icon';
import { themed, useTheme } from './theme';

/**
 * A step of making the combined answer. active: being done now; pending:
 * comes next (grey, read as 'Next step: …'); done; failed.
 */
export type StepView = { key: string; label: string; count?: string; detail?: string; state: 'active' | 'pending' | 'done' | 'failed' };

export function CombinedAnswerCard({
  pile,
  title,
  meta,
  steps,
  info,
  skeleton,
  footnote,
  children,
}: {
  pile: { agents: string[]; dim?: string[]; label: string; onPress: () => void };
  title: string;
  meta?: string;
  steps?: StepView[];
  info?: { label: string; onPress: () => void };
  skeleton?: boolean;
  footnote?: string;
  children?: ReactNode;
}) {
  const th = useTheme();
  const s = styles(th);
  const hasSteps = !!steps?.length;
  const infoButton = info ? <IconButton name="info" label={info.label} onPress={info.onPress} size={18} color={th.caption} /> : null;
  return (
    <View style={s.card}>
      <View style={s.head}>
        <Pressable
          onPress={pile.onPress}
          accessibilityRole="button"
          accessibilityLabel={pile.label}
          style={({ pressed }) => [s.pile, pressed && { backgroundColor: th.raised }]}
        >
          <AvatarPile agents={pile.agents} dim={pile.dim} ring={th.bg} />
          <Icon name="chevronDown" size={14} color={th.caption} />
        </Pressable>
        <Text style={s.title} numberOfLines={1} accessibilityRole="header">
          {title}
        </Text>
        {meta ? (
          // Two lines rather than cut ('answers 11 s · combine…' on a phone,
          // 11-05): each part keeps its words together, so a line breaks only
          // after a ' · ', and two lines fit the row the pile makes tall.
          <Text style={s.meta} numberOfLines={2}>
            {metaParts(meta)}
          </Text>
        ) : null}
        {infoButton ? <View style={[s.headInfo, !meta && s.pushRight]}>{infoButton}</View> : null}
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
  const th = useTheme();
  const s = styles(th);
  if (step.state === 'pending') {
    return (
      <View style={s.step} accessible accessibilityLabel={t('compare.step.next', { step: step.label })}>
        <Icon name="pending" size={16} color={th.caption} />
        <Text style={s.pendingText}>{step.label}</Text>
      </View>
    );
  }
  const words = [step.label, step.count, step.detail].filter(Boolean).join(' ');
  const mark =
    step.state === 'active' ? (
      <Spinner size={16} />
    ) : step.state === 'done' ? (
      <Icon name="check" size={16} color={th.ok} />
    ) : (
      <Icon name="alert" size={16} color={th.danger} />
    );
  return (
    <View style={s.step} accessible accessibilityLabel={words}>
      {mark}
      <Text style={[s.stepText, step.state === 'done' && s.doneText, step.state === 'failed' && s.failedText]}>
        {step.label}
        {step.count ? <Text style={s.count}> {step.count}</Text> : null}
      </Text>
      {step.detail ? <Text style={s.detail}>{step.detail}</Text> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    card: { gap: 8 },
    head: { flexDirection: 'row', alignItems: 'center', gap: 8, marginVertical: -10 },
    pile: { height: 44, flexDirection: 'row', alignItems: 'center', gap: 4, marginLeft: -4, paddingHorizontal: 4, borderRadius: 22 },
    // The title keeps its room; the times give way, onto a second line (English 'answers 23 s · compared in 30 s' is long).
    title: { flexShrink: 0, maxWidth: '60%', fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    meta: { marginLeft: 'auto', flexShrink: 1, minWidth: 0, textAlign: 'right', fontSize: 13, lineHeight: 18, color: th.caption, fontVariant: ['tabular-nums'] },
    headInfo: { marginRight: -14 },
    pushRight: { marginLeft: 'auto' },
    stepsRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 4 },
    steps: { flex: 1, minWidth: 0, gap: 2 },
    step: { minHeight: 28, flexDirection: 'row', alignItems: 'center', gap: 10, flexWrap: 'wrap' },
    stepText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.text },
    doneText: { color: th.textSub },
    failedText: { color: th.danger },
    count: { fontWeight: '400', color: th.textSub, fontVariant: ['tabular-nums'] },
    detail: { fontSize: 13, lineHeight: 18, color: th.caption },
    pendingText: { fontSize: 14, lineHeight: 20, color: th.caption },
    foot: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
    footText: { flex: 1, fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
