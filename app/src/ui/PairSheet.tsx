// The sheet of one Summary item (P2-3), opened from 'See both sentences ›'
// or a Check this item: each side's AIs and sentences one under another
// (differing numbers bold), why it needs a look, the judgment of each
// direction, and a way to each AI's whole answer. No probabilities: who
// judged is what the person can weigh.

import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Comparison } from '@/api/types';
import { labelText, pairView, whyText } from '@/ask/compare';
import { t } from '@/i18n';

import { UnitText } from './CompareItem';
import { type Theme, useTheme } from './theme';

export function PairSheet({
  snap,
  itemKey,
  onClose,
  onShowAnswer,
}: {
  snap?: Comparison;
  itemKey: string | null;
  onClose: () => void;
  /** Show this AI's answer (an agent code, as the tab keys). */
  onShowAnswer: (agent: string) => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const v = snap && itemKey ? pairView(snap, itemKey) : null;
  if (!v) return null;
  return (
    <Modal transparent animationType="slide" visible onRequestClose={onClose}>
      <Pressable style={s.backdrop} onPress={onClose} accessibilityLabel={t('sheet.close')} />
      <View style={s.sheet}>
        <ScrollView contentContainerStyle={s.body}>
          <Text style={s.title}>{t(`compare.kind.${v.item.kind}`)}</Text>
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
          {v.why.length ? (
            <View style={s.section}>
              <Text style={s.heading}>{t('compare.whyTitle')}</Text>
              {v.why.map((w) => (
                <Text key={w} style={s.line}>
                  {whyText(w)}
                </Text>
              ))}
            </View>
          ) : null}
          {v.lines.length ? (
            <View style={s.section}>
              <Text style={s.heading}>{t('compare.judgments')}</Text>
              {v.lines.map((l, i) => (
                <Text key={`${l.unit}/${i}`} style={s.line}>
                  {t('compare.judgment', { judge: l.judge, from: l.from, against: l.against, label: labelText(l.label) })}
                  {l.unsure ? ` · ${t('compare.unsure')}` : ''}
                </Text>
              ))}
            </View>
          ) : null}
          <View style={s.section}>
            {v.agents.map((a) => (
              <Pressable key={a} onPress={() => onShowAnswer(a)} accessibilityRole="button" style={({ pressed }) => [s.action, pressed && s.pressed]}>
                <Text style={s.actionText}>{t('compare.viewAnswer', { name: agentName(a) })}</Text>
              </Pressable>
            ))}
          </View>
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
    body: { padding: 20, gap: 12 },
    title: { color: th.text, fontSize: 17, fontWeight: '700' },
    side: {
      gap: 6,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 10,
      padding: 12,
      backgroundColor: th.bg,
    },
    who: { color: th.text, fontSize: 14, fontWeight: '700' },
    unit: { gap: 2 },
    unitWho: { color: th.muted, fontSize: 12 },
    section: { gap: 6 },
    heading: { color: th.text, fontSize: 14, fontWeight: '700' },
    line: { color: th.text, fontSize: 13, lineHeight: 19 },
    action: { paddingVertical: 4, alignSelf: 'flex-start' },
    actionText: { color: th.accent, fontSize: 14, fontWeight: '600' },
    pressed: { opacity: 0.6 },
    close: { alignItems: 'center', paddingVertical: 12 },
    closeText: { color: th.accent, fontSize: 16, fontWeight: '600' },
  });
