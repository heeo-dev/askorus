// Pictures an AI draws as code (#14): an answer may hold an SVG in a code
// block, and the app shows it as a picture (SvgBlock.tsx). The SVG is
// written by an AI, so it is data, not a page to trust. This file decides
// which SVG may be drawn at all, and builds the page that draws it. The
// page cannot run the SVG's scripts or reach any address: what an SVG could
// load from outside would tell that server the phone's address.
//
// Two walls, each enough alone:
// - drawableSvg refuses an SVG that holds a script, a handler, a frame, HTML
//   inside it, or any address that is not inside the picture itself;
// - the page's Content-Security-Policy allows no connection, no frame, and
//   only the one script built here (named by a nonce new for every page).

/** An SVG longer than this is shown as code: a drawing by an AI is a few kilobytes. */
export const maxSvg = 200_000;

/** The code-block languages an SVG comes in. */
const svgLangs = new Set(['svg', 'xml', 'html', 'image/svg+xml', '']);

// What an SVG that is only a drawing never holds.
const refused: RegExp[] = [
  /<\s*(script|foreignObject|iframe|embed|object|audio|video|link|meta|base|a)\b/i,
  /<!\s*(DOCTYPE|ENTITY)\b/i,
  /<\?xml-stylesheet\b/i,
  // an event handler: on…= as an attribute
  /[\s"'/]on[a-z]+\s*=/i,
  /(javascript|vbscript)\s*:/i,
  /data\s*:\s*text\//i,
  /@import\b/i,
  // CSS that reaches out: url(…) to anything but a part of this picture
  /url\(\s*(?!['"]?#)/i,
];

// An address in an attribute must point inside the picture (#id) or be a
// small picture carried in the SVG itself.
const address = /\b(?:xlink:href|href|src)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/gi;
const inside = /^(#|data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\s]*$)/i;

/**
 * svgOf gives the SVG of a code block when it is one the app may draw: the
 * text from its <svg to its last </svg>, or null (the block is then shown
 * as code, like any other).
 */
export function svgOf(lang: string | undefined, text: string): string | null {
  if (!svgLangs.has((lang ?? '').trim().toLowerCase())) return null;
  if (text.length > maxSvg) return null;
  const start = text.search(/<svg[\s>]/i);
  const end = text.toLowerCase().lastIndexOf('</svg>');
  if (start < 0 || end < start) return null;
  // Only an XML declaration or a comment may come before it, and nothing after.
  const before = text.slice(0, start).replace(/<\?xml\s[^?]*\?>/gi, '').replace(/<!--[\s\S]*?-->/g, '');
  if (before.trim() !== '' || text.slice(end + 6).trim() !== '') return null;
  const svg = text.slice(start, end + 6);
  return drawableSvg(svg) ? svg : null;
}

/** drawableSvg tells whether an SVG is only a drawing: no script, handler, frame, HTML or outside address. */
export function drawableSvg(svg: string): boolean {
  if (svg.length > maxSvg || !/^<svg[\s>]/i.test(svg)) return false;
  if (refused.some((r) => r.test(svg))) return false;
  for (const m of svg.matchAll(address)) {
    const value = (m[1] ?? m[2] ?? m[3] ?? '').trim();
    if (!inside.test(value)) return false;
  }
  return true;
}

const csp = (nonce: string) =>
  ["default-src 'none'", "style-src 'unsafe-inline'", 'img-src data:', `script-src 'nonce-${nonce}'`, "base-uri 'none'", "form-action 'none'"].join('; ');

// Sends the picture's height once it is laid out and whenever it changes.
// window.askorusPng(edge) draws the picture on a canvas (its long edge at
// most edge) and sends it back as a PNG; the app calls it for 'Save'.
const script = `(function () {
  var m = document.getElementById('m'), last = 0;
  function say(o) { if (window.ReactNativeWebView) window.ReactNativeWebView.postMessage(JSON.stringify(o)); }
  function send() {
    var h = Math.ceil(Math.max(m.scrollHeight, m.getBoundingClientRect().height));
    if (h > 0 && h !== last) { last = h; say({ height: h }); }
  }
  send();
  window.addEventListener('load', send);
  if (window.ResizeObserver) new ResizeObserver(send).observe(m); else window.addEventListener('resize', send);
  window.askorusPng = function (edge) {
    try {
      var svg = m.querySelector('svg');
      var box = svg.getBoundingClientRect();
      var w = Math.max(1, box.width), h = Math.max(1, box.height);
      var k = Math.min(4, Math.max(1, edge / Math.max(w, h)));
      var copy = svg.cloneNode(true);
      copy.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
      copy.setAttribute('width', String(w));
      copy.setAttribute('height', String(h));
      var text = new XMLSerializer().serializeToString(copy);
      var img = new Image();
      img.onload = function () {
        try {
          var c = document.createElement('canvas');
          c.width = Math.round(w * k); c.height = Math.round(h * k);
          var g = c.getContext('2d');
          g.fillStyle = '#ffffff'; g.fillRect(0, 0, c.width, c.height);
          g.drawImage(img, 0, 0, c.width, c.height);
          say({ png: c.toDataURL('image/png'), width: c.width, height: c.height });
        } catch (e) { say({ pngError: 1 }); }
      };
      img.onerror = function () { say({ pngError: 1 }); };
      img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(text);
    } catch (e) { say({ pngError: 1 }); }
  };
})();`;

/**
 * svgPage builds the whole page for one picture: the policy, the SVG on a
 * white card (a drawing is made for a white sheet, whatever the app's
 * theme) scaled to the width, and the one script.
 */
export function svgPage(svg: string, nonce: string): string {
  // The nonce goes into an attribute and the policy: only hex is taken.
  if (!/^[0-9a-f]{16,64}$/.test(nonce)) throw new Error('svgPage: a nonce of hex digits is needed');
  const page = [
    'html,body{margin:0;padding:0;background:transparent;overflow:hidden}',
    'body{-webkit-text-size-adjust:100%;-webkit-tap-highlight-color:transparent}',
    '#m{background:#ffffff;border-radius:8px;overflow:hidden;padding:8px;box-sizing:border-box}',
    '#m svg{display:block;max-width:100%;height:auto;margin:0 auto}',
  ].join('\n');
  return [
    '<!DOCTYPE html>',
    '<html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp(nonce)}">`,
    '<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">',
    `<style>\n${page}\n</style>`,
    '</head><body>',
    `<div id="m">${svg}</div>`,
    `<script nonce="${nonce}">${script}</script>`,
    '</body></html>',
  ].join('\n');
}

/** What the page says: its height, a PNG of the picture, or that it could not make one. */
export type SvgMessage = { height: number } | { png: string; width: number; height: number } | { pngError: true };

const maxHeight = 3000;
// A PNG the page sends is base64 after this start; a picture of 4096 px a side stays well under this.
const pngStart = 'data:image/png;base64,';
const maxPng = 24 * 1024 * 1024;

/** svgMessage reads what the page sent, or null when it is not one of its messages. */
export function svgMessage(data: unknown): SvgMessage | null {
  if (typeof data !== 'string' || data.length > maxPng) return null;
  let o: unknown;
  try {
    o = JSON.parse(data);
  } catch {
    return null;
  }
  if (!o || typeof o !== 'object') return null;
  const m = o as Record<string, unknown>;
  if (m.pngError) return { pngError: true };
  if (typeof m.png === 'string') {
    const ok = m.png.startsWith(pngStart) && /^[A-Za-z0-9+/=]+$/.test(m.png.slice(pngStart.length)) && typeof m.width === 'number' && typeof m.height === 'number';
    return ok ? { png: m.png.slice(pngStart.length), width: m.width as number, height: m.height as number } : { pngError: true };
  }
  if (typeof m.height === 'number' && m.height > 0) return { height: Math.min(Math.ceil(m.height), maxHeight) };
  return null;
}
