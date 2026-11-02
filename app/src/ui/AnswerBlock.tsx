// One AI's answer in a turn that is not compared (S5c): a follow-up to one
// AI, a PC whose AIs do not judge, or a turn to two AIs. Its avatar, name,
// model and time, the answer drawn from its Markdown (selectable), and what
// can be done with it: copy, ask only this AI next, and carry Claude's
// conversation on in the Claude app. The same actions are in AnswerActions
// for the answer sheet.

import { StyleSheet, Text, View } from 'react-native';

import { type PartView, tenths } from '@/ask/present';
import { t } from '@/i18n';

import { Avatar } from './Avatar';
import { Button } from './Button';
import { Spinner } from './controls';
import { Markdown } from './Markdown';
import { themed, useTheme } from './theme';

/** What can be done with an answer; a missing one is not offered. */
export type AnswerActionsProps = {
  onCopy?: () => void;
  /** Follow up with this AI only. */
  onOnly?: () => void;
  /** Continue in the Claude app (Claude answers only). */
  onContinue?: () => void;
  note?: { text: string; error?: boolean };
};

/** answerMeta: 'model · 1.2 s', or 'model · Answering…' while it comes. */
export function answerMeta(p: PartView): string {
  if (p.state === 'answering') return [p.model, t('ask.answering')].filter(Boolean).join(' · ');
  return [p.model, p.part.ms !== undefined ? tenths(p.part.ms) : ''].filter(Boolean).join(' · ');
}

/** AnswerBody: the answer, a spinner while it comes, or why there is none. */
export function AnswerBody({ p }: { p: PartView }) {
  const s = styles(useTheme());
  if (p.state === 'answering') {
    return (
      <View style={s.waiting}>
        <Spinner size={16} />
        <Text style={s.caption}>{t('ask.answering')}</Text>
      </View>
    );
  }
  if (p.state === 'failed') return <Text style={s.error}>{p.error}</Text>;
  return <Markdown text={p.part.answer ?? ''} />;
}

/** AnswerActions: copy, only this AI, continue in the Claude app, and what the last one said. */
export function AnswerActions({ onCopy, onOnly, onContinue, note }: AnswerActionsProps) {
  const s = styles(useTheme());
  const any = onCopy || onOnly || onContinue;
  return (
    <>
      {any ? (
        <View style={s.actions}>
          {onCopy ? <Button variant="quiet" icon="copy" label={t('ask.copy')} onPress={onCopy} style={s.action} /> : null}
          {onOnly ? <Button variant="quiet" label={t('ask.onlyThis')} onPress={onOnly} style={s.action} /> : null}
          {onContinue ? <Button variant="quiet" icon="open" label={t('ask.continue')} onPress={onContinue} style={s.action} /> : null}
        </View>
      ) : null}
      {note ? <Text style={note.error ? s.error : s.caption}>{note.text}</Text> : null}
    </>
  );
}

export function AnswerBlock({ p, ...actions }: { p: PartView } & AnswerActionsProps) {
  const s = styles(useTheme());
  const meta = answerMeta(p);
  return (
    <View style={s.block} accessibilityLabel={t('ask.cardLabel', { name: p.name })}>
      <View style={s.head}>
        <Avatar agent={p.agent} />
        <Text style={s.name} numberOfLines={1}>
          {p.name}
        </Text>
        {meta ? (
          <Text style={s.meta} numberOfLines={1}>
            {meta}
          </Text>
        ) : null}
      </View>
      <AnswerBody p={p} />
      {p.state === 'done' ? <AnswerActions {...actions} /> : actions.note ? <AnswerActions note={actions.note} /> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    block: { gap: 8 },
    head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    name: { flexShrink: 0, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    meta: { flex: 1, minWidth: 0, fontSize: 12, lineHeight: 17, color: th.caption },
    waiting: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 28 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
    actions: { flexDirection: 'row', flexWrap: 'wrap', marginLeft: -12, marginVertical: -6 },
    action: { paddingHorizontal: 12 },
  }),
);
