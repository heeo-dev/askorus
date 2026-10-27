// Explains why a PC cannot be used and what to check, then offers retry
// (or pairing again when the PC dropped this phone).

import { router } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';

import { type Cause, causesToCheck } from '@/connections/reach';
import { type MessageKey, t } from '@/i18n';

import { Button } from './Button';
import { type Theme, useTheme } from './theme';

const title: Record<Cause, MessageKey> = {
  unpaired: 'reach.unpaired',
  wrong_account: 'reach.wrong_account',
  agent_down: 'reach.agent_down',
  phone_offline: 'reach.offline',
  pc_unreachable: 'reach.offline',
  other: 'reach.other',
};

const check: Partial<Record<Cause, MessageKey>> = {
  phone_offline: 'reach.check.phone',
  pc_unreachable: 'reach.check.pc',
  agent_down: 'reach.check.agent',
  wrong_account: 'reach.check.account',
};

/** reachTitle is the one-line form, for a PC card. */
export const reachTitle = (c: Cause) => t(title[c]);

export function ReachError({ cause, detail, onRetry }: { cause: Cause; detail?: string; onRetry?: () => void }) {
  const s = styles(useTheme());
  const checks = causesToCheck(cause)
    .map((c) => check[c])
    .filter((k): k is MessageKey => !!k);
  return (
    <View style={s.box} accessibilityRole="alert">
      <Text style={s.title}>{t(title[cause])}</Text>
      {cause === 'other' && detail ? <Text style={s.text}>{detail}</Text> : null}
      {checks.map((k, i) => (
        <Text key={k} style={s.text}>
          {checks.length > 1 ? `${i + 1}. ` : ''}
          {t(k)}
        </Text>
      ))}
      {cause === 'unpaired' ? (
        <Button label={t('sessions.repair')} onPress={() => router.push('/pair')} />
      ) : onRetry ? (
        <Button secondary label={t('reach.retry')} onPress={onRetry} />
      ) : null}
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    box: {
      backgroundColor: th.card,
      borderColor: th.danger,
      borderWidth: 1,
      borderRadius: 12,
      padding: 14,
      gap: 8,
    },
    title: { color: th.danger, fontSize: 16, fontWeight: '700' },
    text: { color: th.text, fontSize: 14, lineHeight: 20 },
  });
