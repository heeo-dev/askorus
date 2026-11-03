// S5c: the palette is the design board's (F-Brand-Tokens, final screens),
// value for value, and every pair of colours that carries words or marks
// reads (WCAG: words 4.5:1, small marks 3:1).

import { aiColour, aiPalettes, contrast, font, palettes, radius, resolveScheme, size, themeChoices } from './theme';

test('the dark and light palettes are the boards’ values', () => {
  expect(palettes.dark).toEqual({
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
  });
  expect(palettes.light).toEqual({
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
  });
  // The old names stay the new colours, so screens not rebuilt keep to the palette.
  for (const p of Object.values(palettes)) {
    expect(p.card).toBe(p.surface);
    expect(p.muted).toBe(p.caption);
  }
});

test('contrast is the WCAG ratio', () => {
  expect(contrast('#000000', '#FFFFFF')).toBeCloseTo(21, 5);
  expect(contrast('#FFFFFF', '#000000')).toBeCloseTo(21, 5);
  expect(contrast('#777777', '#777777')).toBe(1);
  // The board's own figures (F-Brand-Tokens): apricot ink on apricot 9.6, text on the dark bg 15.2.
  expect(contrast('#2A1408', '#FFAD85')).toBeCloseTo(9.6, 1);
  expect(contrast('#EDEBE7', '#151618')).toBeCloseTo(15.2, 1);
  // Why light uses the text colour for apricot words: 1.7:1 and 3.6:1.
  expect(contrast('#FFAD85', '#FCFAF8')).toBeLessThan(2);
  expect(contrast('#DD5A26', '#FCFAF8')).toBeLessThan(4.5);
  expect(() => contrast('rgba(0,0,0,1)', '#FFFFFF')).toThrow();
});

describe.each(['dark', 'light'] as const)('%s: everything with words or marks reads', (scheme) => {
  const p = palettes[scheme];
  const words: Array<[string, string, string, string]> = [
    ['text', p.text, 'bg', p.bg],
    ['text', p.text, 'surface', p.surface],
    ['text', p.text, 'raised', p.raised],
    ['textSub', p.textSub, 'bg', p.bg],
    ['textSub', p.textSub, 'surface', p.surface],
    ['caption', p.caption, 'bg', p.bg],
    ['caption', p.caption, 'surface', p.surface],
    ['onFill', p.onFill, 'fill', p.fill],
    ['accent', p.accent, 'bg', p.bg],
    ['accent', p.accent, 'soft', p.soft],
    ['waitingText', p.waitingText, 'surface', p.surface],
    ['onSelected', p.onSelected, 'selected', p.selected],
    ['onChipOn', p.onChipOn, 'chipOn', p.chipOn],
    ['highlightText', p.highlightText, 'highlightBg', p.highlightBg],
    ['text', p.text, 'differPanel', p.differPanel],
    ['danger', p.danger, 'surface', p.surface],
    ['danger', p.danger, 'bg', p.bg],
    ['text', p.text, 'avatar', p.avatar],
  ];
  test.each(words)('%s on %s ≥ 4.5', (_a, fg, _b, back) => {
    expect(contrast(fg, back)).toBeGreaterThanOrEqual(4.5);
  });
  const marks: Array<[string, string, string, string]> = [
    ['mark', p.mark, 'bg', p.bg],
    ['mark', p.mark, 'surface', p.surface],
    ['ok', p.ok, 'surface', p.surface],
    ['stopped', p.stopped, 'surface', p.surface],
  ];
  test.each(marks)('%s on %s ≥ 3', (_a, fg, _b, back) => {
    expect(contrast(fg, back)).toBeGreaterThanOrEqual(3);
  });
});

describe.each(['dark', 'light'] as const)('%s: each AI’s own colour (#6) reads and is its own', (scheme) => {
  const hues = Object.entries(aiPalettes[scheme]);
  test.each(hues)('%s letters on its tint ≥ 4.5', (_h, c) => {
    expect(contrast(c.fg, c.bg)).toBeGreaterThanOrEqual(4.5);
  });
  test('no AI tint is the plain avatar, apricot or another AI’s', () => {
    const p = palettes[scheme];
    const tints = hues.map(([, c]) => c.bg);
    expect(new Set(tints).size).toBe(tints.length);
    for (const t of tints) expect([p.avatar, p.fill, p.soft, p.highlightBg]).not.toContain(t);
  });
});

test('the AIs we know have their colours, the rest none', () => {
  const th = { ...palettes.dark, scheme: 'dark' as const };
  expect(['claude', 'codex', 'opencode', 'antigravity', 'gemini', 'kimi'].map((a) => aiColour(th, a))).toEqual(
    [aiPalettes.dark.sky, aiPalettes.dark.lilac, aiPalettes.dark.teal, aiPalettes.dark.rose, aiPalettes.dark.rose, aiPalettes.dark.lime],
  );
  for (const a of ['mistral', '', 'toString', 'constructor']) expect(aiColour(th, a)).toBeUndefined();
});

test('the scheme is the choice, or the phone’s; an unknown phone setting is light', () => {
  expect(themeChoices).toEqual(['system', 'dark', 'light']);
  expect(resolveScheme('dark', 'light')).toBe('dark');
  expect(resolveScheme('light', 'dark')).toBe('light');
  expect(resolveScheme('system', 'dark')).toBe('dark');
  expect(resolveScheme('system', 'light')).toBe('light');
  expect(resolveScheme('system', null)).toBe('light');
  expect(resolveScheme('system', undefined)).toBe('light');
  expect(resolveScheme('system', 'unspecified')).toBe('light');
});

test('type, spacing and corners are the boards’', () => {
  expect(font.display).toEqual({ fontSize: 24, fontWeight: '600', lineHeight: 32, letterSpacing: -0.48 });
  expect(font.synth).toEqual({ fontSize: 16, fontWeight: '400', lineHeight: 26 });
  expect(font.avatar).toEqual({ fontSize: 10, fontWeight: '600', lineHeight: 12 });
  expect(size).toMatchObject({ header: 56, hit: 44, gutter: 20, blockGap: 10, cardGap: 8, titleGap: 6, avatar: 24 });
  expect(radius).toMatchObject({ mark: 4, cell: 12, card: 14, group: 16, bubble: 20, bubbleTail: 6, input: 22, sheet: 24 });
  // No font family anywhere: the system font (Noto Sans KR on Android).
  for (const f of Object.values(font)) expect(f).not.toHaveProperty('fontFamily');
});
