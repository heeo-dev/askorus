// Where the AIs said different things, inside the combined answer (S5c,
// F-Result-Expanded): a line in the answer's size, 'AI마다 다릅니다:' and a
// highlighted headline ('Claude 15~20년, Codex 10~15년, …') that opens the
// AIs' own sentences under it, each leading to that AI's answer.
//
// The highlight is a nested Text, so it wraps with the line. Android has no
// dotted underline and no padding or corners on part of a line: there it
// is a solid underline, with thin spaces for room.
//
// The whole line is the button (UX-1): Android gives a Text inside another
// Text no accessibility node of its own unless its role is 'link', so a
// highlight that was the button could not be reached or opened with
// TalkBack, which read the line with its thin spaces and the chevron's
// U+FFFC. The line's label is the lead and the headline; a headline that
// is the count of AIs comes with no lead (UX-6).

import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { NumberSpan } from '@/api/types';
import type { UnitView } from '@/ask/compare';

import { Avatar } from './Avatar';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';
import { UnitText } from './UnitText';

export type DifferRowView = { key: string; agent: string; name: string; unit: UnitView; spans: NumberSpan[]; onPress: () => void };

export function DisagreementHighlight({
  lead,
  headline,
  groupLabel,
  expanded,
  onToggle,
  rows,
}: {
  lead: string;
  headline: string;
  groupLabel: string;
  expanded: boolean;
  onToggle: () => void;
  rows: DifferRowView[];
}) {
  const th = useTheme();
  const s = styles(th);
  // Rows are split by the panel's line on light, by the surface divider on dark (boards).
  const rowLine = th.scheme === 'dark' ? th.divider : th.differLine;
  return (
    <View style={s.box}>
      <Text
        onPress={onToggle}
        accessibilityRole="button"
        accessibilityLabel={lead ? `${lead} ${headline}` : headline}
        accessibilityState={{ expanded }}
        suppressHighlighting
        style={s.line}
      >
        {lead ? `${lead} ` : null}
        <Text style={s.highlight}>
          {' '}
          {headline}
          {' '}
          <Icon name={expanded ? 'chevronUp' : 'chevronDown'} size={14} color={th.highlightText} />
          {' '}
        </Text>
      </Text>
      {expanded ? (
        <View style={s.panel} accessibilityLabel={groupLabel} testID="differ-panel">
          {rows.map((r, i) => (
            <Pressable
              key={r.key}
              onPress={r.onPress}
              accessibilityRole="button"
              accessibilityLabel={`${r.name}: ${r.unit.text}`}
              style={({ pressed }) => [s.row, i > 0 && { borderTopWidth: 1, borderTopColor: rowLine }, pressed && s.pressed]}
            >
              <Avatar agent={r.agent} tone="onDiffer" />
              <View style={s.text}>
                <Text style={s.name}>{r.name}</Text>
                <UnitText unit={r.unit} spans={r.spans} />
              </View>
              <Icon name="chevronRight" size={16} color={th.caption} />
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8 },
    line: { fontSize: 16, lineHeight: 26, color: th.text },
    highlight: {
      color: th.highlightText,
      backgroundColor: th.highlightBg,
      textDecorationLine: 'underline',
      textDecorationStyle: 'dotted',
      textDecorationColor: th.highlightLine,
    },
    panel: { paddingVertical: 2, paddingHorizontal: 4, borderRadius: 14, backgroundColor: th.differPanel, borderWidth: 1, borderColor: th.differLine },
    row: { minHeight: 58, flexDirection: 'row', alignItems: 'center', gap: 10, paddingTop: 7, paddingBottom: 7, paddingLeft: 10, paddingRight: 8 },
    pressed: { opacity: 0.7 },
    text: { flex: 1, minWidth: 0, gap: 1 },
    name: { fontSize: 13, lineHeight: 18.2, color: th.textSub },
  }),
);
