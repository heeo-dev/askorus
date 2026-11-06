// Where a question is written (S5c). Composer is the empty Ask screen's
// box (F-Ask-Empty): the question, the AIs that answer (a pile that opens
// the AI picker), the tier (one chip that opens the tier sheet, #16) and
// the send button. FollowBar is the bar under
// a conversation (F-Ask-Answering, F-Followup-One): a follow-up, its send
// button, and above them (#22, 11-07) the AIs it goes to (the pile, or the
// 'only {name}' chip when it goes to one AI or to the moderator) and its
// tier. Both sit at the bottom of the screen and lift themselves over the
// keyboard.
//
// S5c review: the board is 411 dp wide. Narrower (or with a larger system
// font) the toolbar packs closer: no count beside the pile (its label still
// says it), a tighter pile button; past that the tier chip gives way, so
// the send button never leaves the box (UX-3). The tier was three pills
// whose names were cut at 360 dp (#16); one chip with the picked tier
// fits, and leaves room for more tools. The follow-up grows with what is
// written, up to about five lines (UX-7).

import { Pressable, StyleSheet, Text, TextInput, useWindowDimensions, View } from 'react-native';

import type { Tier } from '@/api/types';
import { t } from '@/i18n';

import { Avatar, AvatarPile } from './Avatar';
import { Icon } from './Icon';
import { useKeyboardInset, useSafeInsets } from './keyboard';
import { themed, type Theme, useTheme } from './theme';

/** composerCompact: the toolbar packs closer when its text has less than 400 dp (the width over the font scale). */
export function composerCompact(width: number, fontScale: number): boolean {
  return width / Math.max(1, fontScale) < 400;
}

function SendButton({ th, canSend, onSend, label, hint }: { th: Theme; canSend: boolean; onSend: () => void; label: string; hint?: string }) {
  return (
    <Pressable
      onPress={onSend}
      disabled={!canSend}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={hint}
      accessibilityState={{ disabled: !canSend }}
      style={({ pressed }) => [
        { width: 44, height: 44, flexShrink: 0, borderRadius: 22, alignItems: 'center', justifyContent: 'center', backgroundColor: canSend ? th.fill : th.raised },
        pressed && { opacity: 0.8 },
      ]}
    >
      <Icon name="send" size={20} color={canSend ? th.onFill : th.caption} />
    </Pressable>
  );
}

export function Composer({
  value,
  onChangeText,
  onSend,
  canSend,
  placeholder,
  agents,
  pickLabel,
  onPickAIs,
  tier,
  onPickTier,
}: {
  value: string;
  onChangeText: (s: string) => void;
  onSend: () => void;
  canSend: boolean;
  placeholder: string;
  agents: string[];
  pickLabel: string;
  onPickAIs: () => void;
  tier: Tier;
  onPickTier: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const lift = Math.max(useKeyboardInset(), useSafeInsets().bottom);
  const { width, fontScale } = useWindowDimensions();
  const compact = composerCompact(width, fontScale);
  return (
    <View style={[s.box, { marginBottom: 12 + lift }]} testID="composer">
      <TextInput
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={th.caption}
        accessibilityLabel={placeholder}
        multiline
        style={s.input}
        textAlignVertical="top"
      />
      <View style={s.tools}>
        <Pressable
          onPress={onPickAIs}
          accessibilityRole="button"
          accessibilityLabel={pickLabel}
          style={({ pressed }) => [s.pick, compact && s.pickCompact, pressed && { backgroundColor: th.raised }]}
        >
          <AvatarPile agents={agents} ring={th.surface} />
          {compact ? null : <Text style={s.count}>{agents.length}</Text>}
          <Icon name="chevronDown" size={14} color={th.textSub} />
        </Pressable>
        <View style={s.divider} />
        <Pressable
          onPress={onPickTier}
          accessibilityRole="button"
          accessibilityLabel={t('ask.tierPick', { tier: t(`ask.tier.${tier}`) })}
          hitSlop={{ top: 6, bottom: 6 }}
          style={({ pressed }) => [s.tier, pressed && { opacity: 0.8 }]}
        >
          <Text style={s.tierText} numberOfLines={1}>
            {t(`ask.tier.${tier}`)}
          </Text>
          <Icon name="chevronDown" size={14} color={th.onChipOn} />
        </Pressable>
        <View style={s.grow} />
        <SendButton th={th} canSend={canSend} onSend={onSend} label={t('ask.send')} />
      </View>
    </View>
  );
}

export function FollowBar({
  value,
  onChangeText,
  onSend,
  canSend,
  placeholder,
  sendLabel,
  sendHint,
  to,
  agents,
  pickLabel,
  onPickAIs,
  tier,
  onPickTier,
}: {
  value: string;
  onChangeText: (s: string) => void;
  onSend: () => void;
  canSend: boolean;
  placeholder: string;
  sendLabel: string;
  sendHint?: string;
  /** One AI, or the moderator, the follow-up goes to alone: a chip in place of the pile. */
  to?: { agent: string; label: string; cancelLabel: string; onCancel: () => void };
  /** The AIs the follow-up goes to, their picker and its tier (#22); left out, the bar has no tools row. */
  agents?: string[];
  pickLabel?: string;
  onPickAIs?: () => void;
  tier?: Tier;
  onPickTier?: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const lift = Math.max(useKeyboardInset(), useSafeInsets().bottom);
  const tools = !!(onPickAIs && tier && onPickTier);
  return (
    <View style={[s.bar, (to || tools) && s.barTo, { paddingBottom: 6 + lift }]} testID="follow-bar">
      {to || tools ? (
        <View style={s.toRow}>
          {to ? (
            <View style={s.to}>
              <Pressable
                onPress={onPickAIs}
                disabled={!onPickAIs}
                accessibilityRole="button"
                accessibilityLabel={pickLabel ?? to.label}
                style={s.toPick}
              >
                <Avatar agent={to.agent} />
                <Text style={s.toText}>{to.label}</Text>
              </Pressable>
              <Pressable
                onPress={to.onCancel}
                accessibilityRole="button"
                accessibilityLabel={to.cancelLabel}
                hitSlop={8}
                style={({ pressed }) => [s.toClose, pressed && { backgroundColor: th.softLine }]}
              >
                <Icon name="close" size={14} color={th.accent} />
              </Pressable>
            </View>
          ) : tools ? (
            <Pressable
              onPress={onPickAIs}
              accessibilityRole="button"
              accessibilityLabel={pickLabel}
              style={({ pressed }) => [s.followPick, pressed && { backgroundColor: th.raised }]}
              testID="follow-pick"
            >
              <AvatarPile agents={agents ?? []} ring={th.bg} />
              <Icon name="chevronDown" size={14} color={th.textSub} />
            </Pressable>
          ) : null}
          {tools && tier ? (
            <Pressable
              onPress={onPickTier}
              accessibilityRole="button"
              accessibilityLabel={t('ask.tierPick', { tier: t(`ask.tier.${tier}`) })}
              hitSlop={{ top: 6, bottom: 6 }}
              style={({ pressed }) => [s.tier, s.followTier, pressed && { opacity: 0.8 }]}
              testID="follow-tier"
            >
              <Text style={s.tierText} numberOfLines={1}>
                {t(`ask.tier.${tier}`)}
              </Text>
              <Icon name="chevronDown" size={14} color={th.onChipOn} />
            </Pressable>
          ) : null}
        </View>
      ) : null}
      <View style={s.followRow}>
        <TextInput
          value={value}
          onChangeText={onChangeText}
          placeholder={placeholder}
          placeholderTextColor={th.caption}
          accessibilityLabel={placeholder}
          multiline
          textAlignVertical="center"
          style={s.followInput}
        />
        <SendButton th={th} canSend={canSend} onSend={onSend} label={sendLabel} hint={sendHint} />
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: {
      marginHorizontal: 12,
      paddingTop: 4,
      paddingHorizontal: 6,
      paddingBottom: 6,
      borderRadius: 22,
      backgroundColor: th.surface,
      borderWidth: 1,
      borderColor: th.border,
    },
    input: {
      minHeight: 64,
      maxHeight: 168,
      paddingTop: 12,
      paddingHorizontal: 12,
      paddingBottom: 4,
      fontSize: 16,
      lineHeight: 24,
      color: th.text,
    },
    tools: { flexDirection: 'row', alignItems: 'center', gap: 2 },
    pick: { height: 44, flexShrink: 0, flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 8, borderRadius: 22 },
    pickCompact: { paddingHorizontal: 4 },
    count: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    divider: { width: 1, height: 20, marginHorizontal: 4, backgroundColor: th.border },
    tier: {
      height: 32,
      flexShrink: 1,
      minWidth: 0,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 4,
      paddingLeft: 12,
      paddingRight: 10,
      borderRadius: 16,
      backgroundColor: th.chipOn,
    },
    tierText: { flexShrink: 1, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.onChipOn },
    grow: { flex: 1 },
    bar: {
      paddingTop: 6,
      paddingRight: 12,
      paddingLeft: 16,
      gap: 6,
      borderTopWidth: 1,
      borderTopColor: th.barLine,
      backgroundColor: th.bg,
    },
    barTo: { paddingTop: 8 },
    toRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    to: { height: 32, flexShrink: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: 2, paddingLeft: 4, paddingRight: 2, borderRadius: 16, backgroundColor: th.soft },
    toPick: { flexShrink: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: 6 },
    followPick: { height: 32, flexShrink: 0, flexDirection: 'row', alignItems: 'center', gap: 4, paddingLeft: 2, paddingRight: 6, borderRadius: 16 },
    followTier: { height: 28, paddingLeft: 10, paddingRight: 8, flexShrink: 0 },
    toText: { fontSize: 13, fontWeight: '600', lineHeight: 18, color: th.accent },
    toClose: { width: 28, height: 28, borderRadius: 14, alignItems: 'center', justifyContent: 'center' },
    // The send button stays at the bottom as the follow-up grows.
    followRow: { flexDirection: 'row', alignItems: 'flex-end', gap: 8 },
    followInput: {
      flex: 1,
      minWidth: 0,
      minHeight: 44,
      maxHeight: 120,
      paddingHorizontal: 18,
      paddingTop: 11,
      paddingBottom: 11,
      borderRadius: 22,
      borderWidth: 1,
      borderColor: th.border,
      backgroundColor: th.surface,
      color: th.text,
      fontSize: 15,
      lineHeight: 20,
    },
  }),
);
