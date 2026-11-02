// Small controls of the boards (S5c): filter chips, segmented choices
// (the composer's tier and Settings' rows), a switch row, pill choices
// with avatars, count marks, status dots, a spinner and skeleton lines.

import { ActivityIndicator, Platform, Pressable, StyleSheet, Text, View } from 'react-native';

import { Avatar } from './Avatar';
import { themed, useTheme } from './theme';

/** Chip is a filter chip (F-PC): 32 high, the picked one on a raised fill. */
export function Chip({ label, selected, onPress, accessibilityLabel }: { label: string; selected: boolean; onPress: () => void; accessibilityLabel?: string }) {
  const s = styles(useTheme());
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityState={{ selected }}
      hitSlop={{ top: 6, bottom: 6 }}
      style={({ pressed }) => [s.chip, selected && s.chipOn, pressed && s.pressed]}
    >
      <Text style={[s.chipText, selected && s.chipTextOn]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  );
}

/**
 * Segment picks one of a few options. look 'tier' is the composer's
 * (F-Ask-Empty: no track, the picked one on a raised fill); 'setting' is a
 * Settings row's (F-Settings: a track, the picked one inverted). dense
 * cuts the tier pills' side padding (a narrow phone's composer); shrink
 * lets the row and its pills give way, their labels cut, when the row has
 * no room left, so what follows it stays inside (UX-3).
 */
export function Segment<T extends string>({
  options,
  value,
  onChange,
  labelOf,
  look,
  label,
  dense,
  shrink,
}: {
  options: readonly T[];
  value: T;
  onChange: (v: T) => void;
  labelOf: (v: T) => string;
  look: 'tier' | 'setting';
  label: string;
  dense?: boolean;
  shrink?: boolean;
}) {
  const s = styles(useTheme());
  const tier = look === 'tier';
  return (
    <View accessibilityRole="radiogroup" accessibilityLabel={label} style={[tier ? s.tierRow : s.track, shrink && s.shrink]}>
      {options.map((o) => {
        const on = o === value;
        return (
          <Pressable
            key={o || '-'}
            onPress={() => onChange(o)}
            accessibilityRole="radio"
            accessibilityLabel={labelOf(o)}
            accessibilityState={{ checked: on }}
            hitSlop={{ top: tier ? 6 : 7, bottom: tier ? 6 : 7 }}
            style={({ pressed }) => [
              tier ? s.tier : s.seg,
              tier && dense && s.tierDense,
              shrink && s.shrink,
              on && (tier ? s.tierOn : s.segOn),
              pressed && !on && s.pressed,
            ]}
          >
            <Text style={[tier ? s.tierText : s.segText, on && (tier ? s.tierTextOn : s.segTextOn)]} numberOfLines={1}>
              {labelOf(o)}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/** SwitchRow is a row that turns something on or off (F-Settings), 52 high. */
export function SwitchRow({ label, value, onChange, disabled }: { label: string; value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <Pressable
      onPress={() => onChange(!value)}
      disabled={disabled}
      accessibilityRole="switch"
      accessibilityLabel={label}
      accessibilityState={{ checked: value, disabled: !!disabled }}
      style={({ pressed }) => [s.switchRow, pressed && s.pressed, disabled && s.off]}
    >
      <Text style={s.switchText}>{label}</Text>
      <View style={[s.track44, { backgroundColor: value ? th.fill : th.stopped, justifyContent: value ? 'flex-end' : 'flex-start' }]}>
        <View style={[s.knob, { backgroundColor: value ? th.onFill : th.bg }]} />
      </View>
    </Pressable>
  );
}

/** PillChoice picks one of a few, as pills with an optional avatar (F-Settings' writer). */
export function PillChoice<T extends string>({
  options,
  value,
  onChange,
  label,
}: {
  options: readonly { value: T; label: string; agent?: string }[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) {
  const s = styles(useTheme());
  return (
    <View accessibilityRole="radiogroup" accessibilityLabel={label} style={s.pills}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Pressable
            key={o.value || '-'}
            onPress={() => onChange(o.value)}
            accessibilityRole="radio"
            accessibilityLabel={o.label}
            accessibilityState={{ checked: on }}
            hitSlop={{ top: 3, bottom: 3 }}
            style={({ pressed }) => [s.pill, !o.agent && s.pillNoAvatar, on ? s.pillOn : s.pillOff, pressed && !on && s.pressed]}
          >
            {o.agent ? <Avatar agent={o.agent} tone={on ? 'inverse' : 'normal'} /> : null}
            <Text style={[s.pillText, on && s.pillTextOn]} numberOfLines={1}>
              {o.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/** Mark is a count on a small square: fill for differ, plain for the other cards. */
export function Mark({ n, tone, size = 'md' }: { n: number | string; tone: 'fill' | 'plain'; size?: 'md' | 'lg' }) {
  const s = styles(useTheme());
  return (
    <View style={[size === 'lg' ? s.markLg : s.markMd, tone === 'fill' ? s.markFill : s.markPlain]}>
      <Text style={[size === 'lg' ? s.markLgText : s.markMdText, tone === 'fill' ? s.markFillText : s.markPlainText]} allowFontScaling={false}>
        {String(n)}
      </Text>
    </View>
  );
}

export type StatusKind = 'waiting' | 'running' | 'ok' | 'stopped' | 'failed';

/**
 * StatusDot is a session's or a PC's state: apricot for waiting for input
 * (with a soft halo in lists), green for running or connected, a ring for
 * stopped, red for failed.
 */
export function StatusDot({ kind, size = 8, halo }: { kind: StatusKind; size?: number; halo?: boolean }) {
  const th = useTheme();
  const colour = kind === 'waiting' ? th.mark : kind === 'failed' ? th.danger : kind === 'stopped' ? 'transparent' : th.ok;
  const dot = (
    <View
      testID={`dot-${kind}`}
      style={[
        { width: size, height: size, borderRadius: size / 2, backgroundColor: colour },
        kind === 'stopped' ? { borderWidth: 1.5, borderColor: th.stopped } : null,
      ]}
    />
  );
  if (!(halo && kind === 'waiting')) return <View importantForAccessibility="no-hide-descendants">{dot}</View>;
  return (
    <View importantForAccessibility="no-hide-descendants" style={{ width: size + 8, height: size + 8, borderRadius: (size + 8) / 2, backgroundColor: th.soft, alignItems: 'center', justifyContent: 'center', margin: -4 }}>
      {dot}
    </View>
  );
}

/** Spinner is the round busy mark (ActivityIndicator) in apricot; a number size only works on Android, so iOS scales the small one. */
export function Spinner({ size = 16, color }: { size?: number; color?: string }) {
  const th = useTheme();
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }} importantForAccessibility="no-hide-descendants">
      {Platform.OS === 'android' ? (
        <ActivityIndicator size={size} color={color ?? th.mark} testID="spinner" />
      ) : (
        <ActivityIndicator size="small" color={color ?? th.mark} style={{ transform: [{ scale: size / 20 }] }} testID="spinner" />
      )}
    </View>
  );
}

/** Skeleton is grey lines standing in for text being written (widths in percent). */
export function Skeleton({ widths }: { widths: number[] }) {
  const s = styles(useTheme());
  return (
    <View style={s.skeleton} accessibilityElementsHidden importantForAccessibility="no-hide-descendants" testID="skeleton">
      {widths.map((w, i) => (
        <View key={i} style={[s.skeletonLine, { width: `${w}%` }]} />
      ))}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    pressed: { opacity: 0.7 },
    off: { opacity: 0.45 },
    chip: { height: 32, borderRadius: 16, paddingHorizontal: 12, justifyContent: 'center' },
    chipOn: { backgroundColor: th.chipOn },
    chipText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.textSub },
    chipTextOn: { fontWeight: '600', color: th.onChipOn },
    tierRow: { flexDirection: 'row', alignItems: 'center' },
    tier: { height: 32, borderRadius: 16, paddingHorizontal: 10, justifyContent: 'center' },
    tierDense: { paddingHorizontal: 6 },
    shrink: { flexShrink: 1, minWidth: 0 },
    tierOn: { backgroundColor: th.chipOn },
    tierText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.textSub },
    tierTextOn: { fontWeight: '600', color: th.onChipOn },
    track: {
      height: 36,
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 3,
      borderRadius: 18,
      backgroundColor: th.bg,
      borderWidth: 1,
      borderColor: th.segLine,
    },
    seg: { height: 30, borderRadius: 15, paddingHorizontal: 11, justifyContent: 'center' },
    segOn: { backgroundColor: th.selected },
    segText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.textSub },
    segTextOn: { fontWeight: '600', color: th.onSelected },
    switchRow: { minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 16, paddingRight: 12 },
    switchText: { flex: 1, fontSize: 15, lineHeight: 22.5, color: th.text },
    track44: { width: 44, height: 26, borderRadius: 13, padding: 3, flexDirection: 'row' },
    knob: { width: 20, height: 20, borderRadius: 10 },
    pills: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    pill: { height: 38, borderRadius: 19, flexDirection: 'row', alignItems: 'center', gap: 6, paddingLeft: 7, paddingRight: 12 },
    pillNoAvatar: { paddingLeft: 12 },
    pillOn: { backgroundColor: th.selected },
    pillOff: { borderWidth: 1, borderColor: th.borderStrong },
    pillText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.text },
    pillTextOn: { fontWeight: '600', color: th.onSelected },
    markMd: { minWidth: 22, height: 22, paddingHorizontal: 6, borderRadius: 4, alignItems: 'center', justifyContent: 'center' },
    markLg: { minWidth: 24, height: 23, paddingHorizontal: 7, marginVertical: 1, borderRadius: 4, alignItems: 'center', justifyContent: 'center' },
    markFill: { backgroundColor: th.fill },
    markPlain: { backgroundColor: th.avatar },
    markMdText: { fontSize: 13, fontWeight: '700', lineHeight: 22, includeFontPadding: false },
    markLgText: { fontSize: 16, fontWeight: '700', lineHeight: 23, includeFontPadding: false },
    markFillText: { color: th.onFill },
    markPlainText: { color: th.text },
    skeleton: { gap: 16, paddingTop: 8, paddingBottom: 4 },
    skeletonLine: { height: 10, borderRadius: 5, backgroundColor: th.skeleton },
  }),
);
