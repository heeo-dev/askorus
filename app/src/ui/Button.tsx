import { Pressable, StyleSheet, Text } from 'react-native';

import { type Theme, useTheme } from './theme';

export function Button({
  label,
  onPress,
  secondary,
  danger,
  disabled,
}: {
  label: string;
  onPress: () => void;
  secondary?: boolean;
  danger?: boolean;
  disabled?: boolean;
}) {
  const s = styles(useTheme());
  const plain = secondary || danger;
  return (
    <Pressable
      style={({ pressed }) => [plain ? s.plain : s.main, disabled && s.disabled, pressed && s.pressed]}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled: !!disabled }}
    >
      <Text style={danger ? s.dangerText : plain ? s.plainText : s.mainText}>{label}</Text>
    </Pressable>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    main: { backgroundColor: th.accent, borderRadius: 10, paddingVertical: 14, alignItems: 'center' },
    mainText: { color: th.accentText, fontSize: 16, fontWeight: '600' },
    plain: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 10,
      paddingVertical: 12,
      paddingHorizontal: 16,
      alignItems: 'center',
      backgroundColor: th.card,
    },
    plainText: { color: th.text, fontSize: 15, fontWeight: '600' },
    dangerText: { color: th.danger, fontSize: 15, fontWeight: '600' },
    disabled: { opacity: 0.5 },
    pressed: { opacity: 0.7 },
  });
