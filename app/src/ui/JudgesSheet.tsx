// The sheet behind 'Judged by: …' in the Summary footer (P2-3): one line
// per judging request, who read whose sentences against whose answer, and
// how it went (the time, still waiting, or why it failed in the AI's words).
// S5b: the combined answer comes first, with the AI that wrote it.

import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import type { Comparison } from '@/api/types';
import { compareErrorText, type RequestLine, requestLines, synthLine } from '@/ask/compare';
import { t } from '@/i18n';

import { type Theme, useTheme } from './theme';

function statusOf(l: RequestLine): { text: string; failed?: boolean } {
  if (!l.judge) return { text: compareErrorText('no_judge'), failed: true };
  switch (l.status) {
    case 'done':
      return { text: [l.ms !== undefined ? t('ask.seconds', { s: (l.ms / 1000).toFixed(1) }) : '', l.model ?? ''].filter(Boolean).join(' · ') };
    case 'queued':
      return { text: t('compare.req.queued') };
    case 'running':
      return { text: t('compare.req.running') };
    case 'failed':
      return { text: compareErrorText(l.error?.code ?? 'internal', l.error?.message), failed: true };
    default:
      return { text: l.status };
  }
}

export function JudgesSheet({ snap, open, onClose }: { snap?: Comparison; open: boolean; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  if (!open) return null;
  const lines = snap ? requestLines(snap) : [];
  const synth = synthLine(snap);
  return (
    <Modal transparent animationType="slide" visible onRequestClose={onClose}>
      <Pressable style={s.backdrop} onPress={onClose} accessibilityLabel={t('sheet.close')} />
      <View style={s.sheet}>
        <ScrollView contentContainerStyle={s.body}>
          <Text style={s.title}>{t('compare.judgesTitle')}</Text>
          {synth ? (
            <View style={s.row}>
              <Text style={s.what}>{synth.what}</Text>
              {synth.text ? <Text style={synth.failed ? s.failed : s.status}>{synth.text}</Text> : null}
            </View>
          ) : null}
          {lines.map((l) => {
            const st = statusOf(l);
            const what = l.judge
              ? t('compare.request', { judge: l.judge, from: l.from, against: l.against }) + (l.judgeOnly ? ` ${t('compare.judgeOnly')}` : '')
              : `${l.from} ↔ ${l.against}`;
            return (
              <View key={l.id} style={s.row}>
                <Text style={s.what}>{what}</Text>
                {st.text ? <Text style={st.failed ? s.failed : s.status}>{st.text}</Text> : null}
              </View>
            );
          })}
          <Pressable onPress={onClose} style={s.close} accessibilityRole="button">
            <Text style={s.closeText}>{t('sheet.close')}</Text>
          </Pressable>
        </ScrollView>
      </View>
    </Modal>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.35)' },
    sheet: { backgroundColor: th.card, maxHeight: '85%', borderTopLeftRadius: 16, borderTopRightRadius: 16 },
    body: { padding: 20, gap: 10 },
    title: { color: th.text, fontSize: 17, fontWeight: '700', marginBottom: 4 },
    row: { gap: 2, paddingBottom: 8, borderBottomColor: th.border, borderBottomWidth: StyleSheet.hairlineWidth },
    what: { color: th.text, fontSize: 14, lineHeight: 20 },
    status: { color: th.muted, fontSize: 13, lineHeight: 18 },
    failed: { color: th.danger, fontSize: 13, lineHeight: 18 },
    close: { alignItems: 'center', paddingVertical: 12 },
    closeText: { color: th.accent, fontSize: 16, fontWeight: '600' },
  });
