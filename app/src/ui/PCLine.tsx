// The PC that answers, as one slim line under the ask screen's header
// (#53). It shows only when more than one PC is paired: with one PC there
// is nothing to mix up. Tapping it opens the PC picker. When opening a
// conversation moved the app to that conversation's PC, the line says so
// for a few seconds (`switched`) instead of asking for a tap on a dialog.
//
// The name gives way when the line is too long, never what tells the PC
// apart: on a phone 'DESKTOP-ABC1234 (Windows · desktop-abc1234)' cut in
// the middle lost the word Windows.

import { Pressable, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';

import { Icon } from './Icon';
import { themed, useTheme } from './theme';

export function PCLine({
  name,
  detail,
  switched,
  onPress,
}: {
  name: string;
  /** What tells this PC from the others (pcDetail); '' when there is nothing to add. */
  detail: string;
  switched: boolean;
  onPress: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const label = detail ? `${name} (${detail})` : name;
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
      {switched ? (
        <Text style={[s.text, s.fill, s.textSwitched]} numberOfLines={3}>
          {words}
        </Text>
      ) : (
        <View style={s.fill} importantForAccessibility="no-hide-descendants">
          <View style={s.parts}>
            <Text style={[s.text, s.name]} numberOfLines={1}>
              {name}
            </Text>
            {detail ? (
              <Text style={[s.text, s.detail]} numberOfLines={1}>
                {detail}
              </Text>
            ) : null}
          </View>
        </View>
      )}
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
    fill: { flex: 1, minWidth: 0 },
    parts: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    text: { fontSize: 13, lineHeight: 18, color: th.textSub },
    name: { flexShrink: 1, color: th.text },
    detail: { flexShrink: 0, maxWidth: '75%' },
    textSwitched: { color: th.text, fontWeight: '500' },
  }),
);
