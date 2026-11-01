// The tabs of a compared turn (P2-3): 정리 (Summary) first, then one per
// asked AI with how its answer stands ('…' still answering, '✕' failed).
// The label stays the bare name (tests find tabs by it); a screen reader
// hears the mark as the tab's value, and '…' as busy.

import { Pressable, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { TabView } from '@/ask/compare';
import { t } from '@/i18n';

import { type Theme, useTheme } from './theme';

export function TurnTabs({ tabs, value, onChange }: { tabs: TabView[]; value: string; onChange: (key: string) => void }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.row} accessibilityRole="tablist">
      {tabs.map((tab) => {
        const label = tab.key === 'summary' ? t('compare.tab.summary') : agentName(tab.agent ?? tab.key);
        const on = tab.key === value;
        return (
          <Pressable
            key={tab.key}
            onPress={() => onChange(tab.key)}
            style={({ pressed }) => [s.tab, on && s.tabOn, pressed && s.pressed]}
            accessibilityRole="tab"
            accessibilityLabel={label}
            accessibilityState={{ selected: on, busy: tab.mark === '…' }}
            accessibilityValue={tab.mark ? { text: t(tab.mark === '…' ? 'ask.answering' : 'compare.tab.failed') } : undefined}
          >
            <Text style={[s.text, on && s.textOn]}>
              {label}
              {tab.mark ? ` ${tab.mark}` : ''}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    row: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
    tab: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 12,
      paddingVertical: 6,
      backgroundColor: th.card,
    },
    tabOn: { backgroundColor: th.accent, borderColor: th.accent },
    text: { color: th.text, fontSize: 14 },
    textOn: { color: th.accentText, fontWeight: '600' },
    pressed: { opacity: 0.7 },
  });
