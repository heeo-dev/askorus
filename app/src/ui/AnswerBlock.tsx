// One AI's answer in a turn that is not compared (S5c): a follow-up to one
// AI, a PC whose AIs do not write, or a turn of two AIs kept from before C6.
// Its avatar, name, model and time, the answer drawn from its Markdown
// (selectable), and what can be done with it: copy, ask only this AI next,
// and carry Claude's conversation on in the Claude app, and behind '...'
// share and report (#50, AnswerMore). The same actions are in AnswerActions
// for the answer sheet. What an AI adds at the end of
// an answer (#4: messages to send next, an offer) is folded under it, as
// the combined answer leaves it out.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { splitExtras } from '@/ask/extras';
import { type PartView, tenths } from '@/ask/present';
import type { ReportSubject } from '@/ask/report';
import { t } from '@/i18n';

import { AnswerMore } from './AnswerMore';
import { Avatar } from './Avatar';
import { Button } from './Button';
import { Spinner } from './controls';
import { IconButton } from './Icon';
import { Markdown } from './Markdown';
import { themed, useTheme } from './theme';

/** What can be done with an answer; a missing one is not offered. */
export type AnswerActionsProps = {
  onCopy?: () => void;
  /** Follow up with this AI only. */
  onOnly?: () => void;
  /** Continue in the Claude app (Claude answers only). */
  onContinue?: () => void;
  /** Share and report, behind '...': the answer's text, who wrote it and the question. */
  more?: ReportSubject;
  note?: { text: string; error?: boolean };
};

/** answerMeta: 'model · 1.2 s', or 'model · Answering…' while it comes. */
export function answerMeta(p: PartView): string {
  if (p.state === 'answering') return [p.model, t('ask.answering')].filter(Boolean).join(' · ');
  return [p.model, p.part.ms !== undefined ? tenths(p.part.ms) : ''].filter(Boolean).join(' · ');
}

/** answerBy: who wrote an answer, for a report: 'Claude sonnet' ('Claude' when the model is not known). */
export function answerBy(p: PartView): string {
  return [p.name, p.model].filter(Boolean).join(' ');
}

/** AnswerBody: the answer, a spinner while it comes, or why there is none. */
export function AnswerBody({ p }: { p: PartView }) {
  const s = styles(useTheme());
  const [extrasOpen, setExtrasOpen] = useState(false);
  if (p.state === 'answering') {
    return (
      <View style={s.waiting}>
        <Spinner size={16} />
        <Text style={s.caption}>{t('ask.answering')}</Text>
      </View>
    );
  }
  if (p.state === 'failed') return <Text style={s.error}>{p.error}</Text>;
  const { main, extras } = splitExtras(p.part.answer ?? '');
  return (
    <>
      <Markdown text={main} />
      {extras ? (
        <View style={s.extras}>
          <Button
            variant="quiet"
            icon={extrasOpen ? 'chevronUp' : 'chevronDown'}
            label={t(extrasOpen ? 'ask.extras.hide' : 'ask.extras.show')}
            onPress={() => setExtrasOpen((o) => !o)}
            style={s.action}
          />
          {extrasOpen ? (
            <>
              <Text style={s.caption}>{t('ask.extras.note')}</Text>
              <Markdown text={extras} />
            </>
          ) : null}
        </View>
      ) : null}
    </>
  );
}

/** AnswerActions: copy, only this AI, continue in the Claude app, '...' (share, report), and what the last one said. */
export function AnswerActions({ onCopy, onOnly, onContinue, more, note }: AnswerActionsProps) {
  const th = useTheme();
  const s = styles(th);
  const [moreOpen, setMoreOpen] = useState(false);
  // What share or report said last (the mail app was opened): shown until the screen says something newer.
  const [said, setSaid] = useState<{ text: string; over?: string } | null>(null);
  const any = onCopy || onOnly || onContinue || more;
  const line = note && note.text !== said?.over ? note : said ? { text: said.text } : note;
  return (
    <>
      {any ? (
        <View style={s.actions}>
          {onCopy ? <Button variant="quiet" icon="copy" label={t('ask.copy')} onPress={onCopy} style={s.action} /> : null}
          {onOnly ? <Button variant="quiet" label={t('ask.onlyThis')} onPress={onOnly} style={s.action} /> : null}
          {onContinue ? <Button variant="quiet" icon="open" label={t('ask.continue')} onPress={onContinue} style={s.action} /> : null}
          {more ? <IconButton name="more" label={t('ask.more')} onPress={() => setMoreOpen(true)} size={20} color={th.textSub} /> : null}
        </View>
      ) : null}
      {line ? <Text style={'error' in line && line.error ? s.error : s.caption}>{line.text}</Text> : null}
      {more && moreOpen ? <AnswerMore subject={more} onClose={() => setMoreOpen(false)} onSaid={(text) => setSaid({ text, over: note?.text })} /> : null}
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
    extras: { gap: 6, alignItems: 'flex-start', marginLeft: -12, paddingLeft: 12 },
    action: { paddingHorizontal: 12 },
  }),
);
