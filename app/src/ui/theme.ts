// The app's look (S5c, decision 8): dark + apricot and light + apricot, the
// tokens of the design board F-Brand-Tokens. The theme follows the phone's
// setting unless the person picks dark or light in Settings (saved by
// appearance.ts).
//
// Apricot #FFAD85 is a fill in both themes (primary buttons, send, switch
// on, the differ mark) with ink #2A1408 on it. As text it reads only on
// dark: on light it is 1.7:1, so `accent` (links, selected words) is the
// text colour there and the small marks (dots, outlines) use deep apricot
// #DD5A26 (`mark`).
//
// card and muted are the old names of surface and caption; accent and
// accentText keep their old meaning (a fill and the text on it, also link
// text), so screens not rebuilt yet stay on the palette.
//
// Each AI we know has a soft colour of its own (#6, aiPalettes); Settings
// can turn it off, back to the one avatar colour of S5c.

import { createContext, createElement, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { type TextStyle, useColorScheme } from 'react-native';
import * as SystemUI from 'expo-system-ui';

import { type KeyValue, secureKeyValue } from '@/connections/store';

import { type Appearance, defaultAppearance, loadAppearance, saveAppearance } from './appearance';

export type Scheme = 'dark' | 'light';
export type ThemeChoice = 'system' | 'dark' | 'light';
export const themeChoices: readonly ThemeChoice[] = ['system', 'dark', 'light'];

export type Palette = {
  bg: string;
  surface: string;
  card: string; // = surface (old name)
  raised: string; // question bubble, raised blocks
  avatar: string;
  border: string; // inputs, composer
  borderStrong: string; // outline buttons, sheet handle, +n avatar
  divider: string; // between rows on a surface
  dividerRaised: string; // between rows on a raised block
  barLine: string; // top line of bottom bars
  segTrack: string;
  segLine: string;
  text: string;
  textSub: string;
  caption: string;
  muted: string; // = caption (old name)
  fill: string; // apricot fills
  onFill: string;
  accent: string; // apricot as text: links, selected words
  accentText: string; // text on `accent` used as a fill (old screens)
  mark: string; // dots, small marks, selected outline, focus
  soft: string; // soft apricot background
  softLine: string;
  waitingText: string;
  highlightBg: string;
  highlightText: string;
  highlightLine: string;
  differPanel: string;
  differLine: string;
  differAvatar: string;
  selected: string;
  onSelected: string;
  chipOn: string;
  onChipOn: string;
  ok: string;
  danger: string;
  stopped: string;
  skeleton: string;
  scrim: string;
  drawerScrim: string;
  shadow: string;
};

const dark: Palette = {
  bg: '#151618',
  surface: '#1E1F22',
  card: '#1E1F22',
  raised: '#2A2B2F',
  avatar: '#2F3034',
  border: '#303136',
  borderStrong: '#3A3B40',
  divider: '#2A2B2F',
  dividerRaised: '#34353A',
  barLine: '#222327',
  segTrack: '#1E1F22',
  segLine: '#2A2B2F',
  text: '#EDEBE7',
  textSub: '#B4B1AB',
  caption: '#8E8B85',
  muted: '#8E8B85',
  fill: '#FFAD85',
  onFill: '#2A1408',
  accent: '#FFAD85',
  accentText: '#2A1408',
  mark: '#FFAD85',
  soft: '#3D2A20',
  softLine: '#3D2A20',
  waitingText: '#FFAD85',
  highlightBg: '#3D2A20',
  highlightText: '#FFAD85',
  highlightLine: '#FFAD85',
  differPanel: '#1E1F22',
  differLine: '#3D2A20',
  differAvatar: '#2F3034',
  selected: '#E4E1DB',
  onSelected: '#17181B',
  chipOn: '#2A2B2F',
  onChipOn: '#EDEBE7',
  ok: '#86C29B',
  danger: '#F0907F',
  stopped: '#8E8B85',
  skeleton: '#2A2B2F',
  scrim: 'rgba(6,6,8,0.64)',
  drawerScrim: 'rgba(0,0,0,0.62)',
  shadow: '#000000',
};

const light: Palette = {
  bg: '#FCFAF8',
  surface: '#F4F2EF',
  card: '#F4F2EF',
  raised: '#EBE8E4',
  avatar: '#E5E1DC',
  border: '#D9D3CD',
  borderStrong: '#D9D3CD',
  divider: '#E8E4DF',
  dividerRaised: '#D9D3CD',
  barLine: '#E8E4DF',
  segTrack: '#F4F2EF',
  segLine: '#E8E4DF',
  text: '#1D1A17',
  textSub: '#524D47',
  caption: '#69635C',
  muted: '#69635C',
  fill: '#FFAD85',
  onFill: '#2A1408',
  accent: '#1D1A17',
  accentText: '#FCFAF8',
  mark: '#DD5A26',
  soft: '#FFE8DC',
  softLine: '#F8CDB8',
  waitingText: '#524D47',
  highlightBg: '#FFAD85',
  highlightText: '#2A1408',
  highlightLine: 'rgba(42,20,8,0.45)',
  differPanel: '#FFE8DC',
  differLine: '#F8CDB8',
  differAvatar: '#FCFAF8',
  selected: '#1D1A17',
  onSelected: '#FCFAF8',
  chipOn: '#E5E1DC',
  onChipOn: '#1D1A17',
  ok: '#2E7D4F',
  danger: '#B23A22',
  stopped: '#69635C',
  skeleton: '#EBE8E4',
  scrim: 'rgba(29,26,23,0.40)',
  drawerScrim: 'rgba(29,26,23,0.40)',
  shadow: '#000000',
};

export const palettes: Record<Scheme, Palette> = { dark, light };

export type Theme = Palette & { scheme: Scheme };

// One object per scheme, so styles can be cached by theme (themed).
const themes: Record<Scheme, Theme> = {
  dark: { ...dark, scheme: 'dark' },
  light: { ...light, scheme: 'light' },
};

/** resolveScheme gives the scheme to draw: the choice, or the phone's (unknown: light). */
export function resolveScheme(choice: ThemeChoice, system: string | null | undefined): Scheme {
  if (choice === 'dark' || choice === 'light') return choice;
  return system === 'dark' ? 'dark' : 'light';
}

// AI colours (#6, board 'AI 색 (#6)' 안 2, soft fills): an AI's avatar and
// its name chips in a combined answer sit on its tint, letters in its deep
// shade (4.5:1 or more). No AI company's own colour, and none near apricot
// (differ) or red (danger). AIs not listed keep the plain avatar.
export type AIHue = 'sky' | 'lilac' | 'teal' | 'rose' | 'lime';
export type AIColour = { bg: string; fg: string };

export const aiPalettes: Record<Scheme, Record<AIHue, AIColour>> = {
  dark: {
    sky: { bg: '#203748', fg: '#9CCDF0' },
    lilac: { bg: '#312C4E', fg: '#C3B4F6' },
    teal: { bg: '#1C3B36', fg: '#84D6C6' },
    rose: { bg: '#43273A', fg: '#F2A5C5' },
    lime: { bg: '#30381F', fg: '#C2D98A' },
  },
  light: {
    sky: { bg: '#DCEDFA', fg: '#1D5C87' },
    lilac: { bg: '#EAE4FB', fg: '#5A3DA6' },
    teal: { bg: '#D5F0EA', fg: '#12665A' },
    rose: { bg: '#FAE1EC', fg: '#9C2D5B' },
    lime: { bg: '#E8F0D2', fg: '#4A5C0F' },
  },
};

const aiHues = new Map<string, AIHue>([
  ['claude', 'sky'],
  ['codex', 'lilac'],
  ['opencode', 'teal'],
  ['antigravity', 'rose'],
  ['gemini', 'rose'],
  ['kimi', 'lime'],
]);

/** aiColour is an AI's own colour in th, or undefined for an AI without one. */
export function aiColour(th: Theme, agent: string): AIColour | undefined {
  const hue = aiHues.get(agent);
  return hue ? aiPalettes[th.scheme][hue] : undefined;
}

type Ctx = {
  theme: Theme;
  choice: ThemeChoice;
  setChoice: (c: ThemeChoice) => void;
  aiColours: boolean;
  setAIColours: (on: boolean) => void;
  ready: boolean;
};
const ThemeContext = createContext<Ctx | null>(null);

/**
 * ThemeProvider reads the saved look once (the theme choice and whether AIs
 * have their own colours) and gives it to every screen under it. Until it
 * is read it draws the phone's scheme with AI colours (ready false). It also
 * sets the window's background (behind the screens, seen during transitions
 * and under the keyboard).
 */
export function ThemeProvider({ children, kv = secureKeyValue }: { children: ReactNode; kv?: KeyValue }) {
  const system = useColorScheme();
  const [look, setLook] = useState<Appearance>(defaultAppearance);
  const [ready, setReady] = useState(false);
  // The latest look, and what the person changed before the saved one was read.
  const latest = useRef<Appearance>(defaultAppearance);
  const touched = useRef(new Set<keyof Appearance>());
  const read = useRef(false);

  useEffect(() => {
    let live = true;
    loadAppearance(kv).then((saved) => {
      if (!live) return;
      // A change made while the saved look was being read wins, and only
      // then is the look saved, so the other setting is not lost.
      const next = { ...saved };
      for (const k of touched.current) Object.assign(next, { [k]: latest.current[k] });
      latest.current = next;
      read.current = true;
      setLook(next);
      setReady(true);
      if (touched.current.size) saveAppearance(next, kv).catch(() => undefined);
    });
    return () => {
      live = false;
    };
  }, [kv]);

  const theme = themes[resolveScheme(look.theme, system)];

  useEffect(() => {
    SystemUI.setBackgroundColorAsync(theme.bg).catch(() => undefined);
  }, [theme.bg]);

  const change = useCallback(
    (patch: Partial<Appearance>) => {
      for (const k of Object.keys(patch) as (keyof Appearance)[]) touched.current.add(k);
      const next = { ...latest.current, ...patch };
      latest.current = next;
      setLook(next);
      if (read.current) saveAppearance(next, kv).catch(() => undefined);
    },
    [kv],
  );
  const setChoice = useCallback((c: ThemeChoice) => change({ theme: c }), [change]);
  const setAIColours = useCallback((on: boolean) => change({ aiColours: on }), [change]);

  const value = useMemo(
    () => ({ theme, choice: look.theme, setChoice, aiColours: look.aiColours, setAIColours, ready }),
    [theme, look, setChoice, setAIColours, ready],
  );
  return createElement(ThemeContext.Provider, { value }, children);
}

/** useTheme gives the theme to draw; outside a ThemeProvider, the phone's scheme. */
export function useTheme(): Theme {
  const ctx = useContext(ThemeContext);
  const system = useColorScheme();
  return ctx ? ctx.theme : themes[resolveScheme('system', system)];
}

/** useThemeChoice is the Settings choice; setChoice saves it. Outside a provider it does nothing. */
export function useThemeChoice(): { choice: ThemeChoice; setChoice: (c: ThemeChoice) => void; ready: boolean } {
  const ctx = useContext(ThemeContext);
  return ctx ? { choice: ctx.choice, setChoice: ctx.setChoice, ready: ctx.ready } : { choice: 'system', setChoice: () => undefined, ready: true };
}

/** useAIColours is whether AIs are drawn in their own colours (Settings); setOn saves it. Outside a provider: on, and setOn does nothing. */
export function useAIColours(): { on: boolean; setOn: (on: boolean) => void } {
  const ctx = useContext(ThemeContext);
  return ctx ? { on: ctx.aiColours, setOn: ctx.setAIColours } : { on: true, setOn: () => undefined };
}

/** themed caches what make builds from a theme (usually a StyleSheet), once per scheme. */
export function themed<T>(make: (th: Theme) => T): (th: Theme) => T {
  const cache = new Map<Theme, T>();
  return (th) => {
    let v = cache.get(th);
    if (v === undefined) {
      v = make(th);
      cache.set(th, v);
    }
    return v;
  };
}

// WCAG relative luminance of #RRGGBB.
function luminance(hex: string): number {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) throw new Error(`not a #RRGGBB colour: ${hex}`);
  const n = parseInt(m[1], 16);
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((c) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

/** contrast is the WCAG contrast ratio of two #RRGGBB colours (1 to 21). */
export function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

// Type (F-Brand-Tokens): the system font (Noto Sans KR on Android), sizes and
// line heights of the boards. lineHeight is in points, letterSpacing too.
export const font = {
  display: { fontSize: 24, fontWeight: '600', lineHeight: 32, letterSpacing: -0.48 },
  title: { fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18 },
  sheetTitle: { fontSize: 17, fontWeight: '600', lineHeight: 23, letterSpacing: -0.17 },
  synth: { fontSize: 16, fontWeight: '400', lineHeight: 26 },
  rowTitle: { fontSize: 16, fontWeight: '500', lineHeight: 22 },
  body: { fontSize: 15, fontWeight: '400', lineHeight: 22.5 },
  listName: { fontSize: 15, fontWeight: '500', lineHeight: 21 },
  block: { fontSize: 14, fontWeight: '600', lineHeight: 20 },
  quote: { fontSize: 14, fontWeight: '400', lineHeight: 20.3 },
  chip: { fontSize: 14, fontWeight: '500', lineHeight: 20 },
  cardBody: { fontSize: 13.5, fontWeight: '400', lineHeight: 20.25 },
  section: { fontSize: 13, fontWeight: '500', lineHeight: 18 },
  caption: { fontSize: 13, fontWeight: '400', lineHeight: 19.5 },
  who: { fontSize: 13, fontWeight: '400', lineHeight: 18.2 },
  small: { fontSize: 12, fontWeight: '400', lineHeight: 17 },
  avatar: { fontSize: 10, fontWeight: '600', lineHeight: 12 },
  markNum: { fontSize: 16, fontWeight: '700', lineHeight: 23 },
  tileNum: { fontSize: 17, fontWeight: '600', lineHeight: 22 },
} satisfies Record<string, TextStyle>;

/** Numbers that line up (times, counts). */
export const tabular: TextStyle = { fontVariant: ['tabular-nums'] };

// Spacing (F-Brand-Tokens 간격): screen side 20, between conversation blocks
// 10, between cards 8, title to content 6, header 56, rows 44/56/64.
export const size = { header: 56, hit: 44, rowS: 44, rowM: 56, rowL: 64, avatar: 24, gutter: 20, blockGap: 10, cardGap: 8, titleGap: 6 };

// Corners (F-Brand-Tokens 모서리).
export const radius = { mark: 4, cell: 12, card: 14, group: 16, bubble: 20, bubbleTail: 6, input: 22, sheet: 24, pill: 999 };
