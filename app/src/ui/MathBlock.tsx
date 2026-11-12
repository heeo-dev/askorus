// Display math ($$…$$, \[…\]) in answers, drawn by KaTeX. KaTeX turns the
// TeX into HTML here on the JS side; a small WebView shows it with KaTeX's
// CSS and fonts built in (katex-style.ts), so nothing is fetched and it runs
// in Expo Go. The page measures itself and sends us its height. TeX that
// KaTeX cannot read, or that could keep it busy for long, is shown as
// written, like a code block.
//
// The WebView only ever shows the page built here: every other load is
// refused, and no URL is handed to another app.

import * as Crypto from 'expo-crypto';
import katex from 'katex';
import { useMemo, useState } from 'react';
import { Platform, ScrollView, StyleSheet, Text, View, useWindowDimensions } from 'react-native';
import { WebView, type WebViewMessageEvent } from 'react-native-webview';

import { katexFonts, katexRules } from './katex-style';
import { type Theme, useTheme } from './theme';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

// About one line with a fraction, until the page has measured itself.
const startHeight = 44;
const maxHeight = 2000;

// The page may run the one script built here, named by a nonce made new for
// every page, and use inline styles and data: fonts, and nothing else;
// base-uri and form-action close the last ways out. With a nonce no other
// inline script and no on… attribute runs, so if KaTeX ever let HTML from a
// formula through, it still could not run code (#47).
const csp = (nonce: string) =>
  [
    "default-src 'none'",
    "style-src 'unsafe-inline'",
    'font-src data:',
    `script-src 'nonce-${nonce}'`,
    'img-src data:',
    "base-uri 'none'",
    "form-action 'none'",
  ].join('; ');

/** newNonce is 128 random bits in hex: the formula was written before it exists, so it cannot name it. */
export function newNonce(): string {
  return [...Crypto.getRandomBytes(16)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// trust: false keeps out \href, \url and \includegraphics; the limits stop a
// formula that expands or grows without end. strict: 'ignore' draws what
// LaTeX would only warn about.
const options = { displayMode: true, throwOnError: true, strict: 'ignore', trust: false, maxSize: 20, maxExpand: 500 } as const;

// KaTeX runs on the JS thread, and its limits do not bound its work:
// maxExpand counts macro expansions, not the symbols they make, and its time
// grows faster than the length. A few hundred characters can freeze the app
// for seconds (longer on Hermes), so this TeX never reaches KaTeX:
// - longer than maxTex (the slowest 3000-char formulas take ~50 ms in Node);
// - defining a macro: one short macro can stand for thousands of symbols;
// - naming an internal command (a name with @): \color and \tag keep their
//   text in \current@color and \df@tag, and typing those names repeats it.
// - \message, \errmessage and \show: KaTeX writes their text to the console,
//   where Expo Go shows an error made up by the answer.
// Names are searched anywhere, not token by token, so \verb cannot hide one
// (\verb\x\\def reads as \verb\x\ then \def). The list is KaTeX 0.19's;
// check it when upgrading katex.
const maxTex = 3000;
const unsafeNames =
  /\\(?:def|gdef|edef|xdef|let|futurelet|global|long|newcommand|renewcommand|providecommand|message|errmessage|show)(?![a-zA-Z@])|\\[a-zA-Z@]*@/;
// Some short TeX grows a hundredfold in HTML (a big matrix, \begin{CD});
// past this the page is too heavy for one formula (a long derivation is
// about 30 KB; an answer may draw 40 of them).
const maxHtml = 200_000;

/** drawable tells whether KaTeX may draw this TeX; other TeX is shown as written. */
export function drawable(tex: string): boolean {
  return tex.length <= maxTex && !unsafeNames.test(tex);
}

// Sends the formula's height after the fonts are in and whenever it changes.
const measure = `(function () {
  var m = document.getElementById('m'), last = 0;
  function send() {
    var h = Math.ceil(Math.max(m.scrollHeight, m.getBoundingClientRect().height));
    if (h > 0 && h !== last && window.ReactNativeWebView) { last = h; window.ReactNativeWebView.postMessage(String(h)); }
  }
  send();
  window.addEventListener('load', send);
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(send);
  if (window.ResizeObserver) new ResizeObserver(send).observe(m); else window.addEventListener('resize', send);
})();`;

/** fontFaces gives the @font-face rules the KaTeX HTML needs, fonts inlined as data: URIs. */
export function fontFaces(html: string): string {
  const used = new Set<string>();
  for (const m of html.matchAll(/class="([^"]*)"/g)) for (const c of m[1].split(/\s+/)) used.add(c);
  return katexFonts
    .filter((f) => f.classes.length === 0 || f.classes.some((c) => used.has(c)))
    .map(
      (f) =>
        `@font-face{font-family:"${f.family}";font-style:${f.style};font-weight:${f.weight};font-display:block;` +
        `src:url(data:font/woff2;base64,${f.data}) format("woff2")}`,
    )
    .join('\n');
}

/** mathPage builds the whole page for one formula: CSP, KaTeX's CSS, the fonts it uses and the HTML. */
export function mathPage(html: string, color: string, fontSize = 15, nonce = newNonce()): string {
  // The nonce goes into an attribute and the CSP: only hex is taken.
  const once = /^[0-9a-f]{16,64}$/.test(nonce) ? nonce : newNonce();
  // The colour goes into CSS, so only a hex colour is taken.
  const ink = /^#[0-9a-f]{3,8}$/i.test(color) ? color : 'inherit';
  // In an HTML page these namespaces are implied; dropping them leaves no web address in the page.
  const body = html.replace(/ xmlns="http:\/\/www\.w3\.org\/[^"]*"/g, '');
  const page = [
    'html,body{margin:0;padding:0;background:transparent;overflow:hidden}',
    `body{color:${ink};font-size:${Math.round(fontSize * 100) / 100}px}`,
    'body{-webkit-text-size-adjust:100%;-webkit-tap-highlight-color:transparent}',
    '#m{overflow-x:auto;overflow-y:hidden;padding:1px 0}',
    '.katex-display{margin:0}',
  ].join('\n');
  return [
    '<!DOCTYPE html>',
    '<html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp(once)}">`,
    '<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">',
    `<style>\n${fontFaces(body)}\n${katexRules}\n${page}\n</style>`,
    '</head><body>',
    `<div id="m">${body}</div>`,
    `<script nonce="${once}">${measure}</script>`,
    '</body></html>',
  ].join('\n');
}

/** pageHeight reads a height the page sent, or null when the message is not one. */
export function pageHeight(data: unknown): number | null {
  if (typeof data !== 'string' || !/^\d{1,6}(\.\d+)?$/.test(data)) return null;
  const h = Math.ceil(Number(data));
  return h > 0 ? Math.min(h, maxHeight) : null;
}

/** allowLoad lets only the page itself load (about:blank on both platforms); links and redirects are refused. */
export function allowLoad({ url }: { url: string }): boolean {
  return url === 'about:blank';
}

// Every URL must reach allowLoad: one that fails originWhitelist is handed to
// Linking.openURL by react-native-webview, which would open another app.
const everyOrigin = ['*'];
const noDetectors: ['none'] = ['none']; // iOS would turn phone numbers into links

function render(tex: string): string | null {
  if (!drawable(tex)) return null; // shown as written
  try {
    const html = katex.renderToString(tex, options);
    return html.length <= maxHtml ? html : null;
  } catch {
    return null; // shown as written
  }
}

/**
 * MathBlock draws one display formula. plain shows it as written without
 * KaTeX (Markdown does that past its budget of formulas for one answer).
 */
export function MathBlock({ tex, plain = false }: { tex: string; plain?: boolean }) {
  const th = useTheme();
  const { fontScale } = useWindowDimensions();
  const html = useMemo(() => (plain ? null : render(tex)), [tex, plain]);
  // Body text scales with the system font size; the formula follows it (textZoom stays at 100).
  const page = useMemo(() => (html === null ? null : mathPage(html, th.text, 15 * fontScale)), [html, th.text, fontScale]);
  const [height, setHeight] = useState(startHeight);
  const [failed, setFailed] = useState<string | null>(null); // the page the WebView could not show

  // Kept as one element so a new height does not render the WebView again,
  // which could make it load the page again.
  const web = useMemo(() => {
    if (page === null) return null;
    const fail = (): void => setFailed(page);
    return (
      <WebView
        source={{ html: page, baseUrl: 'about:blank' }}
        originWhitelist={everyOrigin}
        onShouldStartLoadWithRequest={allowLoad}
        onMessage={(e: WebViewMessageEvent): void => {
          const h = pageHeight(e.nativeEvent.data);
          if (h !== null) setHeight(h);
        }}
        // Android lets a load through when the answer to
        // onShouldStartLoadWithRequest takes longer than 250 ms. The page has
        // no links and its CSP allows no connection, but should any other
        // page ever start to load, the WebView goes and the formula is shown
        // as written (#47).
        onLoadStart={(e): void => {
          if (!allowLoad(e.nativeEvent)) fail();
        }}
        onError={fail}
        onRenderProcessGone={fail}
        onContentProcessDidTerminate={fail}
        // Android: window.open and target=_blank would otherwise open a second
        // WebView with no client, which hands the URL to the browser. With one
        // window they come to allowLoad. CVE-2020-6506, which the docs warn
        // about here, needs a framed page from elsewhere; the CSP allows no frames.
        setSupportMultipleWindows={false}
        javaScriptCanOpenWindowsAutomatically={false}
        allowFileAccess={false}
        // a list: the native props read it as one, and a plain string crashed Android (R-23)
        dataDetectorTypes={noDetectors}
        allowsLinkPreview={false}
        setBuiltInZoomControls={false}
        textZoom={100}
        scrollEnabled={false}
        overScrollMode="never"
        showsVerticalScrollIndicator={false}
        showsHorizontalScrollIndicator={false}
        style={styles.web}
      />
    );
  }, [page]);

  if (web === null || failed === page) {
    const s = sourceStyles(th);
    return (
      <ScrollView horizontal style={s.codeBlock} contentContainerStyle={s.codePad}>
        <Text style={s.codeText} selectable>
          {tex}
        </Text>
      </ScrollView>
    );
  }
  return <View style={{ height }}>{web}</View>;
}

const styles = StyleSheet.create({
  web: {
    backgroundColor: 'transparent',
    // Android: a WebView inside a ScrollView can crash the app while its
    // screen animates away (react-native-webview issues, not in its docs);
    // an opacity just under 1 is the usual way round it.
    ...Platform.select({ android: { opacity: 0.99 }, default: {} }),
  },
});

// The same look as a code block in Markdown.tsx.
const sourceStyles = (th: Theme) =>
  StyleSheet.create({
    codeBlock: { backgroundColor: th.bg, borderRadius: 8, borderColor: th.border, borderWidth: StyleSheet.hairlineWidth },
    codePad: { padding: 10 },
    codeText: { fontFamily: mono, fontSize: 13, lineHeight: 19, color: th.text },
  });
