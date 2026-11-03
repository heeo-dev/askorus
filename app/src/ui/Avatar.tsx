// An AI's round mark (S5c, F-Brand-Tokens 아바타): two letters on a 24 px
// circle, 10/600. Piles overlap 3 px with a 2 px ring of the colour behind
// them, and show at most four before '+n'. Avatars are decoration: the
// control or row they sit in names the AIs. An AI with its own colour (#6)
// is drawn on it wherever it sits, unless that is off in Settings; a dim
// one stays an outline.

import { ActivityIndicator, Platform, StyleSheet, Text, View } from 'react-native';

import { type AIColour, aiColour, themed, type Theme, useAIColours, useTheme } from './theme';

const known: Record<string, string> = { claude: 'Cl', codex: 'Cx', opencode: 'Oc', gemini: 'Ag', antigravity: 'Ag', kimi: 'Ki' };

/** initials are the two letters of an AI's avatar. */
export function initials(agent: string): string {
  if (known[agent]) return known[agent];
  const letters = agent.replace(/[^\p{L}\p{N}]/gu, '');
  if (!letters) return '?';
  return letters.charAt(0).toUpperCase() + letters.charAt(1).toLowerCase();
}

type Tone = 'normal' | 'dim' | 'onDiffer' | 'inverse';

function colours(th: Theme, tone: Tone, ring?: string, own?: AIColour) {
  if (tone === 'dim') return { bg: ring ?? 'transparent', fg: th.caption, border: th.borderStrong };
  if (own) return { bg: own.bg, fg: own.fg };
  switch (tone) {
    case 'onDiffer':
      return { bg: th.differAvatar, fg: th.text };
    case 'inverse':
      return { bg: th.onSelected, fg: th.text };
    default:
      return { bg: th.avatar, fg: th.text };
  }
}

// circle draws the letters; ring adds the 2 px ring outside it (like the
// boards' box-shadow, it takes no room and covers what is behind).
function circle(o: { text: string; size: number; bg: string; fg: string; border?: string; ring?: string }) {
  const { text, size, bg, fg, border, ring } = o;
  const s = size === 28 ? avatarText.big : avatarText.small;
  return (
    <View style={{ width: size, height: size }}>
      {ring ? (
        <View style={{ position: 'absolute', left: -2, top: -2, width: size + 4, height: size + 4, borderRadius: (size + 4) / 2, backgroundColor: ring }} />
      ) : null}
      <View
        style={[
          { width: size, height: size, borderRadius: size / 2, backgroundColor: bg, alignItems: 'center', justifyContent: 'center' },
          border ? { borderWidth: 1, borderColor: border } : null,
        ]}
      >
        <Text style={[s, { color: fg }]} allowFontScaling={false} numberOfLines={1}>
          {text}
        </Text>
      </View>
    </View>
  );
}

const avatarText = StyleSheet.create({
  small: { fontSize: 10, fontWeight: '600', lineHeight: 12, includeFontPadding: false, textAlign: 'center' },
  big: { fontSize: 12, fontWeight: '600', lineHeight: 14, includeFontPadding: false, textAlign: 'center' },
});

const hidden = { accessibilityElementsHidden: true, importantForAccessibility: 'no-hide-descendants' } as const;

export function Avatar({ agent, size = 24, tone = 'normal', ring }: { agent: string; size?: 24 | 28; tone?: Tone; ring?: string }) {
  const th = useTheme();
  const c = colours(th, tone, ring, useAIColours().on ? aiColour(th, agent) : undefined);
  return (
    <View {...hidden} testID={`avatar-${agent}`}>
      {circle({ text: initials(agent), size, bg: c.bg, fg: c.fg, border: c.border, ring })}
    </View>
  );
}

/** AvatarPile shows the AIs overlapping, max of them and then '+n'; dim ones are outlined. */
export function AvatarPile({ agents, max = 4, ring, dim }: { agents: string[]; max?: number; ring: string; dim?: string[] }) {
  const th = useTheme();
  const own = useAIColours().on;
  const shown = agents.slice(0, max);
  const more = agents.length - shown.length;
  return (
    <View {...hidden} style={pile.row} testID="avatar-pile">
      {shown.map((a, i) => {
        const c = colours(th, dim?.includes(a) ? 'dim' : 'normal', ring, own ? aiColour(th, a) : undefined);
        return (
          <View key={`${a}-${i}`} style={i ? pile.next : undefined}>
            {circle({ text: initials(a), size: 24, bg: c.bg, fg: c.fg, border: c.border, ring })}
          </View>
        );
      })}
      {more > 0 ? (
        <View style={pile.next}>{circle({ text: `+${more}`, size: 24, bg: ring, fg: th.textSub, border: th.borderStrong, ring })}</View>
      ) : null}
    </View>
  );
}

const pile = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center' },
  next: { marginLeft: -3 },
});

/**
 * AvatarRing is an AI answering (F-Ask-Answering): its avatar inside a
 * thin track, with a spinner on the track while busy.
 */
export function AvatarRing({ agent, busy }: { agent: string; busy: boolean }) {
  const s = ringStyles(useTheme());
  const th = useTheme();
  return (
    <View style={s.box} {...hidden}>
      <View style={s.track} />
      {busy ? <ActivityIndicator style={s.spin} size={Platform.OS === 'android' ? 32 : 'small'} color={th.mark} testID="avatar-ring-busy" /> : null}
      <Avatar agent={agent} />
    </View>
  );
}

const ringStyles = themed((th) =>
  StyleSheet.create({
    box: { width: 32, height: 32, alignItems: 'center', justifyContent: 'center' },
    track: { position: 'absolute', left: 0, top: 0, width: 32, height: 32, borderRadius: 16, borderWidth: 1.75, borderColor: th.raised },
    spin: { position: 'absolute', left: 0, top: 0, width: 32, height: 32 },
  }),
);
