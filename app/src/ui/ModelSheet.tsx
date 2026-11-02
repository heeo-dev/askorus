// Which model one AI answers with (P2-2, restyled in S5c): by tier (the
// models the PC uses for that AI's tiers, listed under it), or always one
// of those models. Opened from the AI picker. Models show without their
// variant or provider (R-39); when two would then read the same, the full
// name is under them.

import { StyleSheet, View } from 'react-native';

import { agentName } from '@/api/agents';
import { tiers } from '@/api/types';
import { displayModel } from '@/ask/present';
import { t } from '@/i18n';

import { Icon } from './Icon';
import { Group, Row } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

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
    .map((x) => `${t(`ask.tier.${x}`)} ${displayModel(tierModels[x].model)}`)
    .join(' · ');
  const models = [...new Set(tiers.map((x) => tierModels[x]?.model).filter((m): m is string => !!m))];
  if (value && !models.includes(value)) models.push(value);
  const shown = models.map(displayModel);
  const row = (model: string, title: string, subtitle?: string) => {
    const on = model === value;
    return (
      <Row
        key={model || '-'}
        title={title}
        subtitle={subtitle || undefined}
        onPress={() => onPick(model)}
        accessibilityRole="radio"
        accessibilityLabel={title}
        accessibilityState={{ checked: on }}
        accessibilityValue={{ text: subtitle ?? '' }}
        titleStyle={on ? s.on : undefined}
        right={on ? <Icon name="check" size={18} color={th.accent} /> : <View style={s.space} />}
      />
    );
  };
  return (
    <Sheet open onClose={onClose} title={t('ask.modelTitle', { name: agentName(agent) })} testID="model-sheet">
      <View style={s.body}>
        <Group>
          {row('', t('ask.byTier'), byTier)}
          {models.map((m, i) => row(m, shown[i], shown.indexOf(shown[i]) !== shown.lastIndexOf(shown[i]) ? m : undefined))}
        </Group>
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { paddingTop: 6, paddingHorizontal: 16, paddingBottom: 8 },
    on: { fontWeight: '600', color: th.text },
    space: { width: 18 },
  }),
);
