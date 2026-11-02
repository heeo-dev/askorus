// The AIs' own answers as a row of cards to swipe (S5c, F-Ask-Comparing):
// each card shows the AI, its model and the start of its answer, and opens
// the whole answer. The card a follow-up goes to is outlined (F-Followup-One).

import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { Avatar } from './Avatar';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';

export type AICardView = {
  key: string;
  agent: string;
  name: string;
  model?: string;
  preview: string;
  state: 'answering' | 'done' | 'failed';
  selected?: boolean;
  selectedLabel?: string;
  a11yLabel: string;
  onPress: () => void;
};

export function AICardStrip({ title, count, cards }: { title: string; count: string; cards: AICardView[] }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.box}>
      <Text style={s.title} accessibilityRole="header">
        {title}
        <Text style={s.count}> {count}</Text>
      </Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={s.strip} contentContainerStyle={s.stripIn}>
        {cards.map((c) => (
          <Pressable
            key={c.key}
            onPress={c.onPress}
            accessibilityRole="button"
            accessibilityLabel={c.a11yLabel}
            accessibilityState={{ selected: !!c.selected }}
            style={({ pressed }) => [s.card, c.selected && s.cardOn, pressed && s.pressed]}
          >
            <View style={s.head}>
              <Avatar agent={c.agent} />
              <Text style={s.name} numberOfLines={1}>
                {c.name}
              </Text>
              {c.selected && c.selectedLabel ? (
                <View style={s.selected}>
                  <Icon name="check" size={14} color={th.accent} />
                  <Text style={s.selectedText} numberOfLines={1}>
                    {c.selectedLabel}
                  </Text>
                </View>
              ) : c.model ? (
                <Text style={s.model} numberOfLines={1}>
                  {c.model}
                </Text>
              ) : null}
            </View>
            <Text style={[s.preview, c.state === 'failed' && s.failed, c.state === 'answering' && s.answering]} numberOfLines={2}>
              {c.preview}
            </Text>
          </Pressable>
        ))}
      </ScrollView>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 6 },
    title: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    count: { fontWeight: '400' },
    strip: { marginHorizontal: -20 },
    stripIn: { paddingHorizontal: 20, gap: 8 },
    card: {
      width: 236,
      gap: 8,
      // 12/14/14 of the board, less the 1.5 px outline that is always there
      paddingTop: 10.5,
      paddingHorizontal: 12.5,
      paddingBottom: 12.5,
      borderRadius: 14,
      backgroundColor: th.surface,
      borderWidth: 1.5,
      borderColor: 'transparent',
    },
    cardOn: { borderColor: th.mark },
    pressed: { opacity: 0.75 },
    head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    name: { flexShrink: 1, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    model: { marginLeft: 'auto', flexShrink: 1, fontSize: 12, lineHeight: 17, color: th.caption },
    selected: { marginLeft: 'auto', flexDirection: 'row', alignItems: 'center', gap: 4, flexShrink: 1 },
    selectedText: { fontSize: 12, fontWeight: '600', lineHeight: 17, color: th.accent },
    preview: { fontSize: 13.5, lineHeight: 20.25, color: th.textSub },
    answering: { color: th.caption },
    failed: { color: th.danger },
  }),
);
