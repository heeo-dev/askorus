// The question as a bubble at the right (S5c, F-Ask-Answering): raised
// colour, the bottom-right corner tucked in.

import { StyleSheet, Text, View } from 'react-native';

import { themed, useTheme } from './theme';

export function ThreadBubble({ text }: { text: string }) {
  const s = styles(useTheme());
  return (
    <View style={s.row}>
      <View style={s.bubble}>
        <Text style={s.text} selectable>
          {text}
        </Text>
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    row: { flexDirection: 'row', justifyContent: 'flex-end' },
    bubble: {
      maxWidth: 300,
      paddingVertical: 9,
      paddingHorizontal: 16,
      borderTopLeftRadius: 20,
      borderTopRightRadius: 20,
      borderBottomLeftRadius: 20,
      borderBottomRightRadius: 6,
      backgroundColor: th.raised,
    },
    text: { color: th.text, fontSize: 15, lineHeight: 22.5 },
  }),
);
