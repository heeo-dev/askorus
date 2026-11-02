// Grouped rows (S5c, F-Settings and F-Drawer): a rounded block on the
// surface (or raised) colour with a hairline between its rows, the rows
// themselves, and the small grey label over a block.

import { Children, Fragment, isValidElement, type ReactNode } from 'react';
import {
  type AccessibilityRole,
  type AccessibilityState,
  Pressable,
  type StyleProp,
  StyleSheet,
  Text,
  type TextStyle,
  View,
  type ViewStyle,
} from 'react-native';

import { Icon } from './Icon';
import { themed, useTheme } from './theme';

/** Group is a rounded block of rows with a line between them. */
export function Group({ children, tone = 'surface', style }: { children: ReactNode; tone?: 'surface' | 'raised'; style?: StyleProp<ViewStyle> }) {
  const th = useTheme();
  const items = Children.toArray(children).filter((c) => isValidElement(c));
  return (
    <View style={[{ borderRadius: 16, backgroundColor: tone === 'raised' ? th.raised : th.surface, overflow: 'hidden' }, style]}>
      {items.map((c, i) => (
        <Fragment key={(isValidElement(c) && c.key) || i}>
          {i > 0 ? <View style={{ height: 1, backgroundColor: tone === 'raised' ? th.dividerRaised : th.divider }} /> : null}
          {c}
        </Fragment>
      ))}
    </View>
  );
}

export type RowTone = 'caption' | 'sub' | 'waiting' | 'ok' | 'danger';

// Rows without a value say so with an empty one: a value that comes and
// goes stays behind on Android (R-40), so it is always an object.
const noValue = { text: '' };

export function Row({
  title,
  subtitle,
  subtitleTone = 'caption',
  left,
  right,
  chevron,
  onPress,
  onLongPress,
  minHeight = 52,
  titleStyle,
  accessibilityLabel,
  accessibilityRole,
  accessibilityState,
  accessibilityValue,
}: {
  title: string;
  subtitle?: string;
  subtitleTone?: RowTone;
  left?: ReactNode;
  right?: ReactNode;
  chevron?: boolean;
  onPress?: () => void;
  onLongPress?: () => void;
  minHeight?: number;
  titleStyle?: StyleProp<TextStyle>;
  accessibilityLabel?: string;
  accessibilityRole?: AccessibilityRole;
  accessibilityState?: AccessibilityState;
  accessibilityValue?: { text: string };
}) {
  const th = useTheme();
  const s = styles(th);
  const tone = { caption: th.caption, sub: th.textSub, waiting: th.waitingText, ok: th.ok, danger: th.danger }[subtitleTone];
  const body = (
    <>
      {left}
      <View style={s.middle}>
        <Text style={[s.title, titleStyle]}>{title}</Text>
        {subtitle ? <Text style={[s.subtitle, { color: tone }]}>{subtitle}</Text> : null}
      </View>
      {right}
      {chevron ? <Icon name="chevronRight" size={18} color={th.caption} /> : null}
    </>
  );
  const label = accessibilityLabel ?? (subtitle ? `${title}, ${subtitle}` : title);
  if (!onPress && !onLongPress) {
    // A row that only shows things is one element only when asked for (a
    // label or role), so a control at its right stays reachable.
    return (
      <View
        style={[s.row, { minHeight }]}
        accessible={!!(accessibilityLabel || accessibilityRole)}
        accessibilityLabel={label}
        accessibilityRole={accessibilityRole}
        accessibilityState={accessibilityState}
        accessibilityValue={accessibilityValue ?? noValue}
      >
        {body}
      </View>
    );
  }
  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      accessibilityRole={accessibilityRole ?? 'button'}
      accessibilityLabel={label}
      accessibilityState={accessibilityState}
      accessibilityValue={accessibilityValue ?? noValue}
      style={({ pressed }) => [s.row, { minHeight }, pressed && { backgroundColor: th.raised }]}
    >
      {body}
    </Pressable>
  );
}

/** SectionLabel is the small grey name over a block, with something at its right if given. */
export function SectionLabel({ text, right }: { text: string; right?: ReactNode }) {
  const s = styles(useTheme());
  return (
    <View style={s.section}>
      <Text style={s.sectionText} accessibilityRole="header">
        {text}
      </Text>
      {right}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    row: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 16, paddingRight: 12, paddingVertical: 6 },
    middle: { flex: 1, minWidth: 0, gap: 1 },
    title: { fontSize: 15, lineHeight: 21, color: th.text },
    subtitle: { fontSize: 13, lineHeight: 18.2 },
    section: { minHeight: 22, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 4 },
    sectionText: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
  }),
);
