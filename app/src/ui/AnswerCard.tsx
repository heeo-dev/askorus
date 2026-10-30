// One AI's answer in an ask (P2-2): its name, the model and time it took,
// the answer (selectable), and what can be done with it.

import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { RunPart } from '@/api/types';
import { has, type MessageKey, t } from '@/i18n';

import { type Theme, useTheme } from './theme';

/** partError explains why an AI gave no answer; the AI's own message follows. */
export function partError(e: { code: string; message?: string }): string {
  const askKey = `ask.error.${e.code}`;
  const key = `error.${e.code}`;
  const base = has(askKey)
    ? t(askKey as MessageKey, { folder: e.message ?? '' })
    : has(key)
      ? t(key as MessageKey)
      : t('error.unknown', { code: e.code });
  const own = e.code === 'ask_failed' || e.code === 'bad_model' ? e.message : '';
  return own ? `${base}\n${own}` : base;
}

export function AnswerCard({
  part,
  onCopy,
  onOnly,
  onContinue,
  note,
}: {
  part: RunPart;
  onCopy?: () => void;
  /** Follow up with this AI only. */
  onOnly?: () => void;
  /** Continue in the Claude app (Claude answers only). */
  onContinue?: () => void;
  note?: { text: string; error?: boolean };
}) {
  const th = useTheme();
  const s = styles(th);
  const name = agentName(part.agent);
  const done = part.status === 'done';
  // The time goes first: a long model name ("opencode-go/…#high") is what
  // gets cut on a phone, not the time.
  const meta =
    part.status === 'running'
      ? t('ask.answering')
      : [part.ms ? t('ask.seconds', { s: (part.ms / 1000).toFixed(1) }) : '', part.model].filter(Boolean).join(' · ');
  return (
    <View style={s.card} accessibilityLabel={t('ask.cardLabel', { name })}>
      <View style={s.head}>
        <Text style={s.name}>{name}</Text>
        <Text style={s.meta} numberOfLines={1}>
          {meta}
        </Text>
      </View>
      {part.status === 'running' ? (
        <ActivityIndicator color={th.accent} style={s.spinner} />
      ) : done ? (
        <Text style={s.answer} selectable>
          {part.answer}
        </Text>
      ) : (
        <Text style={s.error}>{partError(part.error ?? { code: 'internal' })}</Text>
      )}
      {done ? (
        <View style={s.actions}>
          {onCopy ? <Action th={th} label={t('ask.copy')} onPress={onCopy} /> : null}
          {onOnly ? <Action th={th} label={t('ask.onlyThis')} onPress={onOnly} /> : null}
          {onContinue ? <Action th={th} label={t('ask.continue')} onPress={onContinue} /> : null}
        </View>
      ) : null}
      {note ? <Text style={note.error ? s.error : s.note}>{note.text}</Text> : null}
    </View>
  );
}

function Action({ th, label, onPress }: { th: Theme; label: string; onPress: () => void }) {
  const s = styles(th);
  return (
    <Pressable onPress={onPress} accessibilityRole="button" hitSlop={6} style={({ pressed }) => [s.action, pressed && s.pressed]}>
      <Text style={s.actionText}>{label}</Text>
    </Pressable>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 14,
      gap: 8,
    },
    head: { flexDirection: 'row', alignItems: 'baseline', gap: 8 },
    name: { color: th.text, fontSize: 16, fontWeight: '700' },
    meta: { flex: 1, color: th.muted, fontSize: 12, textAlign: 'right' },
    spinner: { alignSelf: 'flex-start' },
    answer: { color: th.text, fontSize: 15, lineHeight: 22 },
    error: { color: th.danger, fontSize: 14, lineHeight: 20 },
    note: { color: th.muted, fontSize: 13, lineHeight: 19 },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 16, marginTop: 2 },
    action: { paddingVertical: 4 },
    actionText: { color: th.accent, fontSize: 14, fontWeight: '600' },
    pressed: { opacity: 0.6 },
  });
