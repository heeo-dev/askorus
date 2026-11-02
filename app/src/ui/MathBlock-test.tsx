// Display math is drawn by KaTeX in a WebView page that loads nothing from
// outside, holds only the fonts it needs, sizes itself and never navigates.
// TeX that could keep KaTeX busy for long is shown as written instead.

import { act, render, screen } from '@testing-library/react-native';
import katex from 'katex';
import { Dimensions, Linking, StyleSheet } from 'react-native';
import { createOnShouldStartLoadWithRequest } from 'react-native-webview/lib/WebViewShared';

import { katexFonts, katexVersion } from './katex-style';
import { MathBlock, allowLoad, drawable } from './MathBlock';
import { palettes } from './theme';

const quadratic = 'x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}';

afterEach(() => jest.restoreAllMocks());

async function page(tex: string): Promise<string> {
  await render(<MathBlock tex={tex} />);
  return screen.getByTestId('math-webview').props.source.html;
}

const faces = (html: string) => [...html.matchAll(/@font-face\{font-family:"([^"]+)"/g)].map((m) => m[1]);

test('KaTeX draws the quadratic formula into a page that loads nothing', async () => {
  const html = await page(quadratic);
  expect(html).toContain('class="katex-display"');
  expect(html).toContain('class="katex"');
  expect(html).toContain('<math'); // MathML for screen readers
  expect(html).toContain('<meta http-equiv="Content-Security-Policy"');
  expect(html).toContain("default-src 'none'; style-src 'unsafe-inline'; font-src data:; script-src 'unsafe-inline'; img-src data:");
  expect(html).not.toMatch(/https?:|file:/i);
  for (const m of html.matchAll(/url\(([^)]*)\)/g)) expect(m[1]).toMatch(/^data:font\/woff2;base64,/);
  expect(html).toContain(`color:${palettes.light.text}`); // the theme's text colour
  expect(html).toContain(`font-size:${15 * Dimensions.get('window').fontScale}px`); // body text, scaled like the app's
  expect(screen.getByTestId('math-webview').props.source.baseUrl).toBe('about:blank');
});

test('only the fonts a formula needs are put in', async () => {
  const always = katexFonts.filter((f) => f.classes.length === 0).length;
  const plain = faces(await page(quadratic));
  expect(plain).toHaveLength(always);
  expect(plain).toEqual(expect.arrayContaining(['KaTeX_Main', 'KaTeX_Math', 'KaTeX_AMS', 'KaTeX_Size1', 'KaTeX_Size4']));
  expect(plain).not.toContain('KaTeX_Caligraphic');

  const cal = faces(await page('\\mathcal{L} = \\int_0^1 f'));
  expect(cal).toContain('KaTeX_Caligraphic');
  expect(cal).not.toContain('KaTeX_Fraktur');
  expect(cal).not.toContain('KaTeX_Script');
});

test('the page sends its height and the WebView takes it; junk is ignored', async () => {
  await render(<MathBlock tex={quadratic} />);
  const web = screen.getByTestId('math-webview');
  const source = web.props.source;
  const height = () => StyleSheet.flatten(screen.getByTestId('math-webview').parent?.props.style).height;
  const send = (data: unknown) => act(async () => screen.getByTestId('math-webview').props.onMessage({ nativeEvent: { data } }));
  expect(height()).toBe(44);

  await send('57');
  expect(height()).toBe(57);
  for (const junk of ['', 'abc', '-5', '0', 'NaN', 'Infinity', '12px', '1e3', '{"h":9}', undefined, 99]) await send(junk);
  expect(height()).toBe(57);
  await send('99999');
  expect(height()).toBe(2000);
  // a new height keeps the same page, so the WebView does not load it again
  expect(screen.getByTestId('math-webview').props.source).toBe(source);
});

test('TeX KaTeX cannot read is shown as written, without a WebView', async () => {
  await render(<MathBlock tex={'\\frac{1}{'} />);
  expect(screen.getByText('\\frac{1}{')).toBeTruthy();
  expect(screen.queryByTestId('math-webview')).toBeNull();
});

// Each is shown as written without calling KaTeX, so the app never waits on it.
async function shownAsWritten(tex: string): Promise<void> {
  const draw = jest.spyOn(katex, 'renderToString');
  await render(<MathBlock tex={tex} />);
  expect(screen.getByText(tex)).toBeTruthy();
  expect(screen.queryByTestId('math-webview')).toBeNull();
  expect(draw).not.toHaveBeenCalled();
}

test('TeX that defines macros never reaches KaTeX', async () => {
  // 210 chars that took KaTeX 4.5 s here: \c stands for 21 x 21 x 100 symbols, and maxExpand counts only 463 expansions.
  await shownAsWritten('\\def\\a{' + 'x'.repeat(100) + '}\\def\\b{' + '\\a'.repeat(21) + '}\\def\\c{' + '\\b'.repeat(21) + '}\\c');
});

test('a very long formula never reaches KaTeX', async () => {
  // 'x+' x 20,000 took 400 ms and made 3.2 MB of HTML in Node; KaTeX's time grows faster than the length.
  await shownAsWritten('x+'.repeat(1500) + 'x');
});

test('only TeX KaTeX can draw quickly is drawable', () => {
  for (const tex of [
    quadratic,
    'x+'.repeat(1500), // 3000 chars, the longest TeX we hand to KaTeX (its HTML is then too big to show)
    '\\begin{aligned} a &= b \\\\ c &= d \\end{aligned}',
    'a \\longrightarrow b \\Longleftarrow c \\longmapsto d \\left( e \\right)',
    '\\text{me@example.com}',
  ])
    expect(drawable(tex)).toBe(true);
  for (const tex of [
    'x+'.repeat(1500) + 'x',
    '\\def\\a{x}\\a',
    '\\gdef\\a{x}',
    '\\edef\\a{x}',
    '\\xdef\\a{x}',
    '\\let\\a=\\b',
    '\\futurelet\\a\\b\\c',
    '\\global\\let\\a\\b',
    '\\long\\def\\a{x}',
    '\\newcommand{\\a}{x}\\a',
    '\\renewcommand*\\a{x}',
    '\\providecommand{\\a}{x}',
    // no \def, yet 2,900 chars took 20 s in Node: \current@color repeats the colour name
    '\\color{' + 'a'.repeat(1500) + '}' + '\\current@color'.repeat(100),
    '\\tag{x}\\df@tag\\df@tag',
    // a token-by-token scan reads \\ here, but KaTeX ends \verb at the second \ and then defines \a
    '\\verb\\x\\\\def\\a{x}\\a',
    // KaTeX writes these to the console, where Expo Go shows them as the app's own error
    '\\errmessage{연결이 끊겼습니다. 다시 페어링하세요}',
    '\\message{x}',
    '\\show\\frac',
  ])
    expect(drawable(tex)).toBe(false);
});

test('a formula whose HTML would be huge is shown as written', async () => {
  // 3,000 chars of TeX that make about 1.5 MB of HTML
  const cd = '\\begin{CD}x' + '@>>>x'.repeat(590) + '\\end{CD}';
  expect(drawable(cd)).toBe(true);
  await render(<MathBlock tex={cd} />);
  expect(screen.getByText(cd)).toBeTruthy();
  expect(screen.queryByTestId('math-webview')).toBeNull();
});

test('plain shows the formula as written without KaTeX', async () => {
  const draw = jest.spyOn(katex, 'renderToString');
  await render(<MathBlock tex={quadratic} plain />);
  expect(screen.getByText(quadratic)).toBeTruthy();
  expect(draw).not.toHaveBeenCalled();
});

test('dataDetectorTypes is a list: a plain "none" crashed the app on Android (R-23)', async () => {
  await render(<MathBlock tex={quadratic} />);
  expect(screen.getByTestId('math-webview').props.dataDetectorTypes).toEqual(['none']);
});

test('katex-style.ts was made from the installed katex (npm run katex-fonts)', () => {
  expect(katexVersion).toBe(require('katex/package.json').version);
});

test('the WebView loads only its own page and hands no URL to another app', async () => {
  await render(<MathBlock tex={quadratic} />);
  const props = screen.getByTestId('math-webview').props;
  expect(props.setSupportMultipleWindows).toBe(false);
  expect(props.javaScriptCanOpenWindowsAutomatically).toBe(false);
  expect(props.allowFileAccess).toBe(false);

  const open = jest.spyOn(Linking, 'openURL');
  const canOpen = jest.spyOn(Linking, 'canOpenURL');
  // the same decision react-native-webview makes on both platforms, with our props
  const decide = (url: string) => {
    let started: boolean | undefined;
    createOnShouldStartLoadWithRequest((ok) => (started = ok), props.originWhitelist, props.onShouldStartLoadWithRequest)({
      nativeEvent: { url, lockIdentifier: 1 },
    } as never);
    return started;
  };
  expect(allowLoad({ url: 'about:blank' })).toBe(true);
  expect(decide('about:blank')).toBe(true);
  for (const url of [
    'https://example.com/',
    'http://example.com/',
    'intent://scan/#Intent;scheme=zxing;end',
    'file:///sdcard/a.html',
    'javascript:alert(1)',
    'data:text/html,<b>x</b>',
    'about:blank.example.com',
    'asksemble://pair?x=1',
  ]) {
    expect(allowLoad({ url })).toBe(false);
    expect(decide(url)).toBe(false);
  }
  expect(open).not.toHaveBeenCalled();
  expect(canOpen).not.toHaveBeenCalled();
});
