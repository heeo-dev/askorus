// One AI's whole answer as a sheet (S5c), opened from its card or a row of
// the AI sheet: the AI's name, its model and time, the answer drawn from
// its Markdown, and what can be done with it (copy, ask only this AI next,
// continue in the Claude app).

import { StyleSheet, View } from 'react-native';

import type { PartView } from '@/ask/present';
import { t } from '@/i18n';

import { AnswerActions, type AnswerActionsProps, AnswerBody, answerMeta } from './AnswerBlock';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function AnswerSheet({ p, onClose, ...actions }: { p: PartView | null; onClose: () => void } & AnswerActionsProps) {
  const s = styles(useTheme());
  if (!p) return null;
  return (
    <Sheet open onClose={onClose} title={p.name} subtitle={answerMeta(p) || undefined} testID="answer-sheet">
      <View style={s.body} accessibilityLabel={t('ask.cardLabel', { name: p.name })}>
        <AnswerBody p={p} />
        {p.state === 'done' ? <AnswerActions {...actions} /> : actions.note ? <AnswerActions note={actions.note} /> : null}
      </View>
    </Sheet>
  );
}

const styles = themed(() =>
  StyleSheet.create({
    body: { gap: 10, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
  }),
);
