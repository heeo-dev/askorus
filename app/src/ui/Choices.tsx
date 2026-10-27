import { Pressable, StyleSheet, Text, View } from 'react-native';

import { type Theme, useTheme } from './theme';

/** Choices is a row of chips where one is picked. */
export function Choices<T extends string>({
  options,
  value,
  onChange,
  labelOf,
}: {
  options: readonly T[];
  value: T;
  onChange: (v: T) => void;
  labelOf: (v: T) => string;
}) {
  const s = styles(useTheme());
  return (
    <View style={s.row} accessibilityRole="radiogroup">
      {options.map((o) => {
        const on = o === value;
        return (
          <Pressable
            key={o || '-'}
            onPress={() => onChange(o)}
            style={[s.chip, on && s.chipOn]}
            accessibilityRole="radio"
            accessibilityState={{ checked: on }}
          >
            <Text style={[s.text, on && s.textOn]}>{labelOf(o)}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    row: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    chip: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 14,
      paddingVertical: 7,
      backgroundColor: th.card,
    },
    chipOn: { backgroundColor: th.accent, borderColor: th.accent },
    text: { color: th.text, fontSize: 14 },
    textOn: { color: th.accentText, fontWeight: '600' },
  });
