// Which model one AI answers with (P2-2, restyled in S5c; efforts and the
// own pick 11-06). Opened from the AI picker:
// - by tier: the model and effort the PC uses at each tier, listed under it;
// - one of the tiers' model-and-effort pairs, each with its effort ('haiku ·
//   medium'), so two efforts of one model (OpenCode's Flash at low and at
//   high) are two rows;
// - an own pick: one of the tiers' models and any effort the PC lists for
//   it, as pills that apply at once (the sheet stays open for the other).
// Models show without their variant or provider (R-39). What to list comes
// from ask/models.ts.

import { StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { tiers, type Tier } from '@/api/types';
import {
  effortChoices,
  effortOf,
  type ModelEfforts,
  modelLabel,
  type ModelPick,
  modelSpoken,
  pickModels,
  presets,
  runsWith,
  samePick,
  type TierModels,
  withEffort,
  withModel,
} from '@/ask/models';
import { displayModel } from '@/ask/present';
import { t } from '@/i18n';

import { PillChoice } from './controls';
import { Icon } from './Icon';
import { Group, Row } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function ModelSheet({
  agent,
  tierModels,
  efforts,
  tier,
  value,
  onPick,
  onClose,
}: {
  agent: string | null;
  tierModels: TierModels;
  efforts: ModelEfforts;
  /** The tier asked at now: an own pick starts from its model. */
  tier: Tier;
  /** The person's pick; null follows the tier. */
  value: ModelPick | null;
  /** close: a row was picked (back to the picker); the own pick's pills keep the sheet open. */
  onPick: (pick: ModelPick | null, close: boolean) => void;
  onClose: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  if (!agent) return null;

  const byTier = tiers
    .filter((x) => tierModels[x]?.model)
    .map((x) => `${t(`ask.tier.${x}`)} ${modelLabel(tierModels[x])}`)
    .join('\n');
  const rows = presets(tierModels);

  // The own pick: the tiers' models (and a picked one the PC no longer
  // lists), and the efforts of the model picked, or of the tier's.
  const models = pickModels(tierModels);
  if (value && !models.includes(value.model)) models.push(value.model);
  const effortModel = value?.model ?? runsWith(null, tierModels, tier)?.model ?? models[0] ?? '';
  const choices = effortModel ? effortChoices(tierModels, efforts, effortModel) : [];
  const picked = value ? effortOf(value) : '';
  if (picked && !choices.includes(picked)) choices.push(picked);
  const own = !!value && !rows.some((r) => samePick(r.pick, value));

  const row = (key: string, pick: ModelPick | null, title: string, subtitle: string | undefined, spoken: string) => {
    const on = pick === null ? value === null : !!value && samePick(pick, value);
    return (
      <Row
        key={key}
        title={title}
        subtitle={subtitle || undefined}
        onPress={() => onPick(pick, true)}
        accessibilityRole="radio"
        accessibilityLabel={spoken}
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
          {row('-', null, t('ask.byTier'), byTier, t('ask.byTier'))}
          {rows.map((r) =>
            row(
              `${r.pick.model}#${effortOf(r.pick)}`,
              r.pick,
              modelLabel(r.pick),
              r.tiers.map((x) => t(`ask.tier.${x}`)).join(' · '),
              modelSpoken(r.pick),
            ),
          )}
        </Group>
        <View style={s.own}>
          <View style={s.ownHead}>
            <Text style={[s.ownTitle, own && s.on]} accessibilityRole="header">
              {t('ask.ownPick')}
            </Text>
            <Text style={s.caption}>{t('ask.ownPickHint')}</Text>
          </View>
          <Text style={s.label}>{t('ask.ownModel')}</Text>
          <PillChoice
            label={`${t('ask.ownPick')}: ${t('ask.ownModel')}`}
            options={models.map((m) => ({ value: m, label: displayModel(m) }))}
            value={value?.model ?? ''}
            onChange={(m) => onPick(withModel(tierModels, efforts, value, m), false)}
          />
          {choices.length ? (
            <>
              <Text style={s.label}>{t('ask.ownEffort')}</Text>
              <PillChoice
                label={`${t('ask.ownPick')}: ${t('ask.ownEffort')}`}
                options={choices.map((e) => ({ value: e, label: e }))}
                value={picked}
                onChange={(e) => onPick(withEffort(value, effortModel, e), false)}
              />
            </>
          ) : null}
        </View>
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { paddingTop: 6, paddingHorizontal: 16, paddingBottom: 16, gap: 20 },
    on: { fontWeight: '600', color: th.text },
    space: { width: 18 },
    own: { gap: 10, paddingHorizontal: 4 },
    ownHead: { gap: 2 },
    ownTitle: { fontSize: 16, fontWeight: '500', lineHeight: 22, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    label: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.textSub, marginTop: 4 },
  }),
);
