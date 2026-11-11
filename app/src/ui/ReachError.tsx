// Explains why a PC cannot be used and what to check, then offers retry
// (or pairing again when the PC dropped this phone). Used by 내 PC and the
// Ask screen (S5c: a surface block with a danger title, as the boards'
// tokens; the words and the order are unchanged). With several PCs (#53)
// it names the PC it is about and offers picking another. How to start
// the agent is the same on every system (#52: `askorus-agent start`).

import { router } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';

import { type Cause, causesToCheck } from '@/connections/reach';
import { type MessageKey, t } from '@/i18n';

import { Button } from './Button';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';

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

/** reachTitle is the one-line form, for a PC row. */
export const reachTitle = (c: Cause) => t(title[c]);

export function ReachError({
  cause,
  detail,
  onRetry,
  pc,
  onPick,
}: {
  cause: Cause;
  detail?: string;
  onRetry?: () => void;
  /** The PC this is about, in words that tell it from the others; given while several are paired. */
  pc?: string;
  /** Opens the PC picker; given while several are paired. */
  onPick?: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const checks = causesToCheck(cause)
    .map((c) => check[c])
    .filter((k): k is MessageKey => !!k);
  return (
    <View style={s.box} accessibilityRole="alert">
      <View style={s.head}>
        <Icon name="alert" size={18} color={th.danger} style={s.icon} />
        <Text style={s.title}>{t(title[cause])}</Text>
      </View>
      {pc ? <Text style={s.pc}>{t('ask.onPC', { name: pc })}</Text> : null}
      {cause === 'other' && detail ? <Text style={s.text}>{detail}</Text> : null}
      {checks.map((k, i) => (
        <Text key={k} style={s.text}>
          {checks.length > 1 ? `${i + 1}. ` : ''}
          {t(k)}
        </Text>
      ))}
      {cause === 'unpaired' ? (
        <Button label={t('sessions.repair')} onPress={() => router.push('/pair')} style={s.button} />
      ) : onRetry ? (
        <Button variant="outline" icon="retry" label={t('reach.retry')} onPress={onRetry} style={s.button} />
      ) : null}
      {onPick ? <Button variant="outline" icon="switch" label={t('pc.pick')} onPress={onPick} /> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8, padding: 16, borderRadius: 14, backgroundColor: th.surface, borderWidth: 1, borderColor: th.softLine },
    head: { flexDirection: 'row', alignItems: 'flex-start', gap: 8 },
    icon: { marginTop: 1 },
    title: { flex: 1, fontSize: 15, fontWeight: '600', lineHeight: 21, color: th.danger },
    text: { fontSize: 14, lineHeight: 20.3, color: th.textSub },
    pc: { fontSize: 14, fontWeight: '500', lineHeight: 20.3, color: th.text },
    button: { marginTop: 4 },
  }),
);
