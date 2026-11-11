// The PC that answers, as one slim line under the ask screen's header
// (#53). It shows only when more than one PC is paired: with one PC there
// is nothing to mix up. Tapping it opens the PC picker. When opening a
// conversation moved the app to that conversation's PC, the line says so
// for a few seconds (`switched`) instead of asking for a tap on a dialog.

import { Pressable, StyleSheet, Text } from 'react-native';

import { t } from '@/i18n';

import { Icon } from './Icon';
import { themed, useTheme } from './theme';

export function PCLine({ label, switched, onPress }: { label: string; switched: boolean; onPress: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const words = switched ? t('ask.pcSwitched', { name: label }) : label;
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${switched ? words : t('ask.onPC', { name: label })}, ${t('pc.pick')}`}
      accessibilityLiveRegion={switched ? 'polite' : 'none'}
      hitSlop={{ top: 6, bottom: 6 }}
      style={({ pressed }) => [s.line, switched && s.switched, pressed && s.pressed]}
      testID="pc-line"
    >
      <Icon name="pc" size={16} color={switched ? th.text : th.textSub} />
      <Text style={[s.text, switched && s.textSwitched]} numberOfLines={switched ? 2 : 1} ellipsizeMode="middle">
        {words}
      </Text>
      <Icon name="switch" size={16} color={th.textSub} />
    </Pressable>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    line: {
      minHeight: 36,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      marginHorizontal: 12,
      marginBottom: 4,
      paddingHorizontal: 12,
      paddingVertical: 6,
      borderRadius: 12,
    },
    switched: { backgroundColor: th.raised },
    pressed: { backgroundColor: th.raised },
    text: { flex: 1, minWidth: 0, fontSize: 13, lineHeight: 18, color: th.textSub },
    textSwitched: { color: th.text, fontWeight: '500' },
  }),
);
