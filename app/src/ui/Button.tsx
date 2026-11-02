// Buttons (S5c): primary (apricot fill, 52 high), outline (48), quiet (44,
// no frame) and danger (outline with danger words). `secondary` and
// `danger` are the old props, kept for screens not rebuilt yet.

import { ActivityIndicator, Pressable, type StyleProp, StyleSheet, Text, View, type ViewStyle } from 'react-native';

import { Icon, type IconName } from './Icon';
import { themed, useTheme } from './theme';

export type ButtonVariant = 'primary' | 'outline' | 'quiet' | 'danger';

export function Button({
  label,
  onPress,
  variant,
  secondary,
  danger,
  icon,
  disabled,
  busy,
  accessibilityLabel,
  accessibilityHint,
  style,
}: {
  label: string;
  onPress: () => void;
  variant?: ButtonVariant;
  secondary?: boolean;
  danger?: boolean;
  icon?: IconName;
  disabled?: boolean;
  busy?: boolean;
  accessibilityLabel?: string;
  accessibilityHint?: string;
  style?: StyleProp<ViewStyle>;
}) {
  const th = useTheme();
  const s = styles(th);
  const v: ButtonVariant = variant ?? (danger ? 'danger' : secondary ? 'outline' : 'primary');
  const off = !!disabled || !!busy;
  const colour = v === 'primary' ? th.onFill : v === 'danger' ? th.danger : v === 'quiet' ? th.textSub : th.text;
  return (
    <Pressable
      onPress={onPress}
      disabled={off}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityHint={accessibilityHint}
      accessibilityState={{ disabled: off, busy: !!busy }}
      style={({ pressed }) => [s.base, s[v], pressed && s.pressed, off && s.off, style]}
    >
      <View style={s.inner}>
        {busy ? <ActivityIndicator size="small" color={colour} /> : icon ? <Icon name={icon} size={v === 'quiet' ? 18 : 20} color={colour} /> : null}
        <Text style={[s.label, s[`${v}Text`], { color: colour }]} numberOfLines={2}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    base: { alignItems: 'center', justifyContent: 'center' },
    inner: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8 },
    primary: { minHeight: 52, borderRadius: 26, paddingHorizontal: 22, backgroundColor: th.fill },
    outline: { minHeight: 48, borderRadius: 24, paddingHorizontal: 18, borderWidth: 1, borderColor: th.borderStrong },
    danger: { minHeight: 48, borderRadius: 24, paddingHorizontal: 18, borderWidth: 1, borderColor: th.borderStrong },
    quiet: { minHeight: 44, borderRadius: 22, paddingHorizontal: 12 },
    label: { textAlign: 'center' },
    primaryText: { fontSize: 16, fontWeight: '600', lineHeight: 22 },
    outlineText: { fontSize: 15, fontWeight: '500', lineHeight: 21 },
    dangerText: { fontSize: 15, fontWeight: '500', lineHeight: 21 },
    quietText: { fontSize: 14, fontWeight: '500', lineHeight: 20 },
    pressed: { opacity: 0.7 },
    off: { opacity: 0.45 },
  }),
);
