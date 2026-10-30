// The sheet a long-pressed AI chip opens (P2-2): answer by tier, or always
// with one of the models the PC uses for that AI's tiers.

import { Modal, Pressable, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { tiers } from '@/api/types';
import { t } from '@/i18n';

import { type Theme, useTheme } from './theme';

export function ModelSheet({
  agent,
  tierModels,
  value,
  onPick,
  onClose,
}: {
  agent: string | null;
  tierModels: Record<string, { model: string; effort?: string }>;
  value: string; // '' = by tier
  onPick: (model: string) => void;
  onClose: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  if (!agent) return null;
  const byTier = tiers
    .filter((x) => tierModels[x])
    .map((x) => `${t(`ask.tier.${x}`)} ${tierModels[x].model}`)
    .join(' · ');
  const models = [...new Set(tiers.map((x) => tierModels[x]?.model).filter((m): m is string => !!m))];
  if (value && !models.includes(value)) models.push(value);
  const row = (model: string, text: string, sub?: string) => {
    const on = model === value;
    return (
      <Pressable
        key={model || '-'}
        onPress={() => onPick(model)}
        style={({ pressed }) => [s.row, on && s.rowOn, pressed && s.pressed]}
        accessibilityRole="radio"
        accessibilityState={{ checked: on }}
        accessibilityLabel={text}
      >
        <Text style={[s.rowText, on && s.rowTextOn]}>{text}</Text>
        {sub ? <Text style={s.sub}>{sub}</Text> : null}
      </Pressable>
    );
  };
  return (
    <Modal transparent animationType="slide" visible onRequestClose={onClose}>
      <Pressable style={s.backdrop} onPress={onClose} accessibilityLabel={t('sheet.close')} />
      <View style={s.sheet}>
        <Text style={s.title}>{t('ask.modelTitle', { name: agentName(agent) })}</Text>
        {row('', t('ask.byTier'), byTier)}
        {models.map((m) => row(m, m))}
        <Pressable onPress={onClose} style={s.close} accessibilityRole="button">
          <Text style={s.closeText}>{t('sheet.close')}</Text>
        </Pressable>
      </View>
    </Modal>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.35)' },
    sheet: { backgroundColor: th.card, padding: 20, gap: 8, borderTopLeftRadius: 16, borderTopRightRadius: 16 },
    title: { color: th.text, fontSize: 17, fontWeight: '700', marginBottom: 4 },
    row: { borderColor: th.border, borderWidth: 1, borderRadius: 10, padding: 12, gap: 2 },
    rowOn: { borderColor: th.accent, backgroundColor: th.bg },
    rowText: { color: th.text, fontSize: 15 },
    rowTextOn: { color: th.accent, fontWeight: '700' },
    sub: { color: th.muted, fontSize: 12 },
    pressed: { opacity: 0.7 },
    close: { alignItems: 'center', paddingVertical: 12 },
    closeText: { color: th.accent, fontSize: 16, fontWeight: '600' },
  });
