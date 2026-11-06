// Which tier a question goes at (#16): opened from the composer's tier chip.
// Each tier lists the models the AIs that are on use at it (an AI given a
// model of its own in the AI picker answers with that at every tier, so it
// is not listed).

import { StyleSheet, View } from 'react-native';

import { tiers, type Tier } from '@/api/types';
import { t } from '@/i18n';

import { Icon } from './Icon';
import { Group, Row } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function TierSheet({
  open,
  value,
  models,
  onPick,
  onClose,
}: {
  open: boolean;
  value: Tier;
  /** models: per tier, a line like 'Claude haiku · medium, Codex gpt-6-luna · medium' ('' when none follow the tier). */
  models: Record<Tier, string>;
  onPick: (tier: Tier) => void;
  onClose: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  return (
    <Sheet open={open} onClose={onClose} title={t('ask.tierLabel')} testID="tier-sheet">
      <View style={s.body}>
        <Group>
          {tiers.map((x) => {
            const on = x === value;
            const title = t(`ask.tier.${x}`);
            return (
              <Row
                key={x}
                title={title}
                subtitle={models[x] || undefined}
                onPress={() => onPick(x)}
                accessibilityRole="radio"
                accessibilityLabel={title}
                accessibilityState={{ checked: on }}
                accessibilityValue={{ text: models[x] }}
                titleStyle={on ? s.on : undefined}
                right={on ? <Icon name="check" size={18} color={th.accent} /> : <View style={s.space} />}
              />
            );
          })}
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
