// The combined answer at the top of the 정리 (Summary) tab (P2-3 S5b): one
// AI's write-up of every answer and the comparison, drawn from its Markdown
// like an answer (selectable, links only http(s)). Under it who wrote it
// (the link opens the Judged by sheet), that it can be wrong, and the
// notes the PC gives: written without comparison results, or some places
// where the AIs differ left out. While it is written it says so; when it
// failed, why and 'Write again'; when it was off, 'Write a combined answer'
// and what that costs. All of it comes from compare.ts (synthState).

import { Pressable, StyleSheet, Text, View } from 'react-native';

import { type SynthState, synthBy, synthErrorText } from '@/ask/compare';
import { t } from '@/i18n';

import { Button } from './Button';
import { Markdown } from './Markdown';
import { type Theme, useTheme } from './theme';

export function SynthesisCard({
  state,
  onWrite,
  onCopy,
  onBy,
  note,
}: {
  state: SynthState;
  /** Write it (off) or write it again (failed). */
  onWrite: () => void;
  onCopy: (text: string) => void;
  /** Open the Judged by sheet. */
  onBy: () => void;
  note?: { text: string; error?: boolean };
}) {
  const th = useTheme();
  const s = styles(th);

  if (state.kind === 'off') {
    return (
      <View style={s.off}>
        <Button secondary label={t('compare.synth.write')} onPress={onWrite} />
        <Text style={s.muted}>{t('compare.synth.cost', { n: state.cost })}</Text>
      </View>
    );
  }

  const title = (
    <Text style={s.title} accessibilityRole="header">
      {t('compare.synth.title')}
    </Text>
  );

  if (state.kind === 'waiting' || state.kind === 'writing') {
    return (
      <View style={s.card}>
        {title}
        <Text style={s.muted}>{t('compare.synth.writing')}</Text>
      </View>
    );
  }

  if (state.kind === 'failed') {
    return (
      <View style={s.card}>
        {title}
        <Text style={s.error}>{synthErrorText(state.code, state.message)}</Text>
        {state.retry ? <Button secondary label={t('compare.synth.rewrite')} onPress={onWrite} /> : null}
      </View>
    );
  }

  const by = synthBy(state);
  return (
    <View style={s.card}>
      {title}
      <Markdown text={state.text} />
      <View style={s.actions}>
        <Pressable
          onPress={() => onCopy(state.text)}
          accessibilityRole="button"
          hitSlop={6}
          style={({ pressed }) => [s.action, pressed && s.pressed]}
        >
          <Text style={s.actionText}>{t('ask.copy')}</Text>
        </Pressable>
      </View>
      {note ? <Text style={note.error ? s.error : s.note}>{note.text}</Text> : null}
      {by ? (
        <Pressable onPress={onBy} accessibilityRole="button" hitSlop={6} style={({ pressed }) => pressed && s.pressed}>
          <Text style={s.link}>{t('compare.synth.by', { list: by })} ›</Text>
        </Pressable>
      ) : null}
      <Text style={s.small}>{t('compare.synth.mayErr')}</Text>
      {!state.judged ? <Text style={s.small}>{t('compare.synth.noJudge')}</Text> : null}
      {state.unnamed > 0 ? <Text style={s.warn}>{t('compare.synth.unnamed')}</Text> : null}
    </View>
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
    off: { gap: 6 },
    title: { color: th.text, fontSize: 16, fontWeight: '700' },
    muted: { color: th.muted, fontSize: 14, lineHeight: 20 },
    small: { color: th.muted, fontSize: 12, lineHeight: 17 },
    // A note to act on: in the text colour, so it does not read as fine print.
    warn: { color: th.text, fontSize: 13, lineHeight: 18, fontWeight: '600' },
    error: { color: th.danger, fontSize: 14, lineHeight: 20 },
    note: { color: th.muted, fontSize: 13, lineHeight: 19 },
    link: { color: th.accent, fontSize: 13, lineHeight: 18, fontWeight: '600' },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 16, marginTop: 2 },
    action: { paddingVertical: 4 },
    actionText: { color: th.accent, fontSize: 14, fontWeight: '600' },
    pressed: { opacity: 0.6 },
  });
