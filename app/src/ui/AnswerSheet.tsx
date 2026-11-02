// One AI's whole answer as a sheet (S5c), opened from its card, a row of
// the AI sheet, a differ row or a pair sheet: the AI's name, its model and
// time, who judged it, the answer drawn from its Markdown, and what can be
// done with it (copy, ask only this AI next, continue in the Claude app).

import { StyleSheet, Text, View } from 'react-native';

import type { PartView } from '@/ask/present';
import { t } from '@/i18n';

import { AnswerActions, type AnswerActionsProps, AnswerBody, answerMeta } from './AnswerBlock';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function AnswerSheet({
  p,
  judgedBy,
  onClose,
  ...actions
}: { p: PartView | null; judgedBy: string[]; onClose: () => void } & AnswerActionsProps) {
  const s = styles(useTheme());
  if (!p) return null;
  return (
    <Sheet open onClose={onClose} title={p.name} subtitle={answerMeta(p) || undefined} testID="answer-sheet">
      <View style={s.body} accessibilityLabel={t('ask.cardLabel', { name: p.name })}>
        {judgedBy.length ? <Text style={s.caption}>{t('compare.aiLine', { names: judgedBy.join(', ') })}</Text> : null}
        <AnswerBody p={p} />
        {p.state === 'done' ? <AnswerActions {...actions} /> : actions.note ? <AnswerActions note={actions.note} /> : null}
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 10, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
