// Pictures an AI draws as code (#14): an answer may hold an SVG in a code
// block, and the app shows it as a picture (SvgBlock.tsx). The SVG is
// written by an AI, so it is data, not a page to trust. The page that draws
// it must run none of the SVG's code and reach no address: what an SVG
// could load from outside would tell that server the phone's address.
//
// Three walls:
// - cleanSvg reads the SVG and writes it again from what it knows: the
//   elements and attributes of a drawing, with every value read as the
//   browser will read it (entities decoded) and checked. What the page gets
//   is the rewritten text, never the AI's own. Anything cleanSvg cannot read
//   or does not know as part of a drawing leaves the block as code (R-121);
// - the page's Content-Security-Policy allows no connection, no frame, and
//   only the one script built here (named by a nonce new for every page);
// - the WebView loads only that page (SvgBlock.tsx).

/** An SVG longer than this is shown as code: a drawing by an AI is a few kilobytes. */
export const maxSvg = 200_000;
/** A drawing with more elements, or nested deeper, than this is shown as code. */
const maxElements = 4000;
const maxDepth = 64;

/** The code-block languages an SVG comes in. */
const svgLangs = new Set(['svg', 'xml', 'html', 'image/svg+xml', '']);

const byLower = (names: string[]) => new Map(names.map((n) => [n.toLowerCase(), n]));

// The elements of a drawing, as SVG spells them. Not here: script, a (a
// link), foreignObject (HTML inside), and every HTML element. An HTML
// element inside an SVG would make the browser leave the SVG and read HTML.
const elements = byLower([
  'svg', 'g', 'defs', 'symbol', 'use', 'switch',
  'path', 'rect', 'circle', 'ellipse', 'line', 'polyline', 'polygon', 'image',
  'text', 'tspan', 'textPath',
  'linearGradient', 'radialGradient', 'stop', 'pattern', 'clipPath', 'mask', 'marker',
  'filter', 'feBlend', 'feColorMatrix', 'feComponentTransfer', 'feComposite', 'feConvolveMatrix', 'feDiffuseLighting',
  'feDisplacementMap', 'feDistantLight', 'feDropShadow', 'feFlood', 'feFuncA', 'feFuncB', 'feFuncG', 'feFuncR',
  'feGaussianBlur', 'feImage', 'feMerge', 'feMergeNode', 'feMorphology', 'feOffset', 'fePointLight',
  'feSpecularLighting', 'feSpotLight', 'feTile', 'feTurbulence',
  'style', 'animate', 'animateTransform', 'animateMotion', 'set', 'mpath',
]); // prettier-ignore

// Read and left out with all they hold: they draw nothing, and the browser
// reads what is inside title and desc as HTML.
const leftOut = new Set(['title', 'desc', 'metadata']);

// The elements that change an attribute over time.
const animating = new Set(['animate', 'animateTransform', 'animateMotion', 'set']);

// The attributes of a drawing. Any other attribute (a handler, data-…, a
// namespace) is dropped.
const attributes = byLower([
  'id', 'class', 'style', 'lang', 'transform', 'transform-origin', 'viewBox', 'preserveAspectRatio', 'version',
  'x', 'y', 'x1', 'y1', 'x2', 'y2', 'cx', 'cy', 'r', 'rx', 'ry', 'width', 'height', 'd', 'points', 'pathLength',
  'dx', 'dy', 'rotate', 'textLength', 'lengthAdjust', 'startOffset', 'method', 'spacing', 'side',
  'fill', 'fill-opacity', 'fill-rule', 'stroke', 'stroke-width', 'stroke-linecap', 'stroke-linejoin', 'stroke-dasharray',
  'stroke-dashoffset', 'stroke-miterlimit', 'stroke-opacity', 'opacity', 'color', 'display', 'visibility', 'overflow',
  'clip-path', 'clip-rule', 'mask', 'filter', 'marker-start', 'marker-mid', 'marker-end', 'paint-order', 'vector-effect',
  'shape-rendering', 'text-rendering', 'image-rendering', 'color-interpolation', 'color-interpolation-filters',
  'mix-blend-mode', 'isolation',
  'font', 'font-family', 'font-size', 'font-weight', 'font-style', 'font-variant', 'font-stretch', 'text-anchor',
  'dominant-baseline', 'alignment-baseline', 'baseline-shift', 'letter-spacing', 'word-spacing', 'text-decoration',
  'writing-mode', 'direction', 'unicode-bidi',
  'offset', 'stop-color', 'stop-opacity', 'gradientUnits', 'gradientTransform', 'spreadMethod', 'fx', 'fy', 'fr',
  'patternUnits', 'patternContentUnits', 'patternTransform', 'clipPathUnits', 'maskUnits', 'maskContentUnits',
  'markerWidth', 'markerHeight', 'markerUnits', 'refX', 'refY', 'orient',
  'href', 'xlink:href',
  'filterUnits', 'primitiveUnits', 'in', 'in2', 'result', 'stdDeviation', 'mode', 'type', 'values', 'operator',
  'k1', 'k2', 'k3', 'k4', 'flood-color', 'flood-opacity', 'lighting-color', 'surfaceScale', 'diffuseConstant',
  'specularConstant', 'specularExponent', 'kernelMatrix', 'order', 'divisor', 'bias', 'targetX', 'targetY', 'edgeMode',
  'preserveAlpha', 'kernelUnitLength', 'baseFrequency', 'numOctaves', 'seed', 'stitchTiles', 'scale',
  'xChannelSelector', 'yChannelSelector', 'radius', 'azimuth', 'elevation', 'z', 'pointsAtX', 'pointsAtY', 'pointsAtZ',
  'limitingConeAngle', 'tableValues', 'slope', 'intercept', 'amplitude', 'exponent',
  'attributeName', 'attributeType', 'from', 'to', 'by', 'begin', 'dur', 'end', 'repeatCount', 'repeatDur', 'calcMode',
  'keyTimes', 'keySplines', 'keyPoints', 'additive', 'accumulate', 'restart', 'min', 'max', 'path',
]); // prettier-ignore

// What an animation may not change: where an element points, its name, its
// CSS as a whole.
const notAnimated = new Set(['href', 'xlink:href', 'id', 'class', 'style', 'attributeName', 'attributeType']);

// An address must point inside the picture (#id) or be a small picture
// carried in the SVG itself.
const inside = /^(#[^\s#]*|data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\s]*)$/i;

// The few names an AI writes besides XML's five; any other &name; stays text.
const named: Record<string, string> = {
  amp: '&', lt: '<', gt: '>', quot: '"', apos: "'",
  nbsp: ' ', copy: '©', reg: '®', deg: '°', middot: '·', times: '×',
  ndash: '–', mdash: '—', hellip: '…', larr: '←', rarr: '→', bull: '•',
}; // prettier-ignore

/** decode reads the entities of a value or a text as the browser will; null when one is not a character a drawing holds. */
function decode(s: string): string | null {
  let bad = false;
  const out = s.replace(/&(#x[0-9a-f]{1,6}|#[0-9]{1,7}|[a-z]{2,8});/gi, (all, name: string) => {
    if (name[0] !== '#') return named[name] ?? named[name.toLowerCase()] ?? all;
    const code = name[1] === 'x' || name[1] === 'X' ? parseInt(name.slice(2), 16) : parseInt(name.slice(1), 10);
    if (!(code > 0 && code <= 0x10ffff) || (code >= 0xd800 && code <= 0xdfff)) {
      bad = true;
      return '';
    }
    return String.fromCodePoint(code);
  });
  // No control characters but tab and the line ends.
  // eslint-disable-next-line no-control-regex
  return bad || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(out) ? null : out;
}

const escText = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const escAttr = (s: string) => escText(s).replace(/"/g, '&quot;');

// What a value or a style sheet of a drawing never holds. A backslash is
// CSS's way to spell a letter by its number ("u\72l("), "//" starts an
// address, and the functions and rules named here load something.
const reaching: RegExp[] = [
  /\\/,
  /\/\//,
  /url\(\s*(?!['"]?#)/i,
  /@\s*(import|font-face|namespace|charset)\b/i,
  /(image-set|cross-fade|image|element|src|expression)\s*\(/i,
  /(javascript|vbscript|data)\s*:/i,
  /-moz-binding|behavior\s*:/i,
];

/** contained tells whether a decoded value or style sheet reaches nowhere outside the picture. */
const contained = (v: string) => !reaching.some((r) => r.test(v));

const tagName = /[A-Za-z][A-Za-z0-9:._-]*/y;
const attribute = /\s+([^\s"'<>/=]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/y;
const tagEnd = /\s*(\/?)>/y;

/**
 * cleanSvg reads one SVG and writes it again as a drawing only, or gives
 * null when it is not one: text it cannot read as XML, an element that is
 * not part of a drawing (a script, a link, HTML), an address or a style
 * that reaches outside the picture. Attributes it does not know, comments
 * and the title, desc and metadata elements are left out.
 */
export function cleanSvg(svg: string): string | null {
  if (svg.length > maxSvg) return null;
  const out: string[] = [];
  const open: string[] = []; // the open elements, lower case
  let skipFrom = -1; // while >= 0: inside a left-out element opened at this depth
  let count = 0;
  let closed = false; // the root has ended
  let i = 0;

  const text = (raw: string, decoded: boolean): boolean => {
    const value = decoded ? raw : decode(raw);
    if (value === null) return false;
    if (value.trim() === '') {
      if (skipFrom < 0 && open.length > 0) out.push(value);
      return true;
    }
    if (open.length === 0) return false; // text outside the picture
    if (skipFrom >= 0) return true;
    if (open[open.length - 1] === 'style' && !contained(value)) return false;
    out.push(escText(value));
    return true;
  };

  while (i < svg.length) {
    if (svg[i] !== '<') {
      const next = svg.indexOf('<', i);
      const end = next < 0 ? svg.length : next;
      if (!text(svg.slice(i, end), false)) return null;
      i = end;
      continue;
    }
    if (svg.startsWith('<!--', i)) {
      const end = svg.indexOf('-->', i + 4);
      if (end < 0) return null;
      i = end + 3;
      continue;
    }
    if (svg.startsWith('<![CDATA[', i)) {
      const end = svg.indexOf(']]>', i + 9);
      // eslint-disable-next-line no-control-regex
      if (end < 0 || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(svg.slice(i + 9, end))) return null;
      if (!text(svg.slice(i + 9, end), true)) return null;
      i = end + 3;
      continue;
    }
    if (svg.startsWith('</', i)) {
      tagName.lastIndex = i + 2;
      const m = tagName.exec(svg);
      if (!m) return null;
      const name = m[0].toLowerCase();
      tagEnd.lastIndex = tagName.lastIndex;
      const e = tagEnd.exec(svg);
      if (!e || e[1] !== '' || open.pop() !== name) return null;
      if (skipFrom >= 0) {
        if (open.length === skipFrom) skipFrom = -1;
      } else {
        out.push(`</${elements.get(name)}>`);
      }
      if (open.length === 0) closed = true;
      i = tagEnd.lastIndex;
      continue;
    }
    // An element starts. Anything else after "<" (a DOCTYPE, an instruction, a stray "<") is not a drawing.
    tagName.lastIndex = i + 1;
    const m = tagName.exec(svg);
    if (!m || closed) return null;
    const name = m[0].toLowerCase();
    const known = elements.get(name);
    const skipping = skipFrom >= 0;
    if (!skipping && !known && !leftOut.has(name)) return null;
    if (open.length === 0 && name !== 'svg') return null;
    if (++count > maxElements) return null;

    const attrs: string[] = [];
    const seen = new Set<string>();
    let at = tagName.lastIndex;
    for (;;) {
      attribute.lastIndex = at;
      const a = attribute.exec(svg);
      if (!a) break;
      at = attribute.lastIndex;
      const key = a[1].toLowerCase();
      if (seen.has(key)) return null; // the browser and XML would not agree which one counts
      seen.add(key);
      // A handler is code: the picture is more than a drawing, and stays code.
      if (/^on/.test(key) && !skipping) return null;
      const value = decode(a[2] ?? a[3] ?? '');
      if (value === null) return null;
      const attr = attributes.get(key);
      if (skipping || !known || !attr) continue;
      if (attr === 'href' || attr === 'xlink:href') {
        if (!inside.test(value.trim())) return null;
        attrs.push(` ${attr}="${escAttr(value.trim())}"`);
        continue;
      }
      if (!contained(value)) return null;
      if (attr === 'attributeName' && animating.has(name)) {
        const target = attributes.get(value.trim().toLowerCase());
        if (!target || notAnimated.has(target)) return null;
      }
      attrs.push(` ${attr}="${escAttr(value)}"`);
    }
    tagEnd.lastIndex = at;
    const e = tagEnd.exec(svg);
    if (!e) return null;
    i = tagEnd.lastIndex;
    const selfClosed = e[1] === '/';
    // An animation must say what it changes, or it changes what its parent says.
    if (!skipping && known && animating.has(name) && name !== 'animatemotion' && !seen.has('attributename')) return null;
    if (!skipping && known) out.push(`<${known}${attrs.join('')}${selfClosed ? '/' : ''}>`);
    if (selfClosed) {
      if (open.length === 0) closed = true;
      continue;
    }
    if (open.length >= maxDepth) return null;
    if (!skipping && !known) skipFrom = open.length;
    open.push(name);
  }
  if (!closed || open.length > 0) return null;
  return out.join('');
}

/**
 * svgOf gives the picture of a code block when it is one the app may draw:
 * the block's SVG, written again as a drawing only (cleanSvg), or null (the
 * block is then shown as code, like any other).
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
  return cleanSvg(text.slice(start, end + 6));
}

/** drawableSvg tells whether an SVG is only a drawing. */
export function drawableSvg(svg: string): boolean {
  return cleanSvg(svg) !== null;
}

const csp = (nonce: string) =>
  ["default-src 'none'", "style-src 'unsafe-inline'", 'img-src data:', `script-src 'nonce-${nonce}'`, "base-uri 'none'", "form-action 'none'"].join('; ');

/** The longest side, in CSS pixels, of a picture the page will turn into a PNG. */
export const maxPictureSide = 8192;

// Sends the picture's height once it is laid out and whenever it changes.
// window.askorusPng(edge) draws the picture on a canvas whose long edge is
// at most edge (and at most four times the picture) and sends it back as a
// PNG; the app calls it for 'Save'. The size is settled before the canvas
// is made: a picture that says it is huge gets a small canvas or none.
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
      var w = box.width, h = box.height;
      edge = Math.min(Math.max(Number(edge) || 0, 16), 4096);
      if (!(w >= 1 && h >= 1 && w <= ${maxPictureSide} && h <= ${maxPictureSide})) { say({ pngError: 1 }); return; }
      var k = Math.min(4, edge / Math.max(w, h));
      var cw = Math.max(1, Math.round(w * k)), ch = Math.max(1, Math.round(h * k));
      var copy = svg.cloneNode(true);
      copy.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
      copy.setAttribute('width', String(w));
      copy.setAttribute('height', String(h));
      var text = new XMLSerializer().serializeToString(copy);
      var img = new Image();
      img.onload = function () {
        try {
          var c = document.createElement('canvas');
          c.width = cw; c.height = ch;
          var g = c.getContext('2d');
          g.fillStyle = '#ffffff'; g.fillRect(0, 0, cw, ch);
          g.drawImage(img, 0, 0, cw, ch);
          say({ png: c.toDataURL('image/png'), width: cw, height: ch });
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
 * theme) scaled to the width, and the one script. The SVG goes through
 * cleanSvg here too, so no caller can put an AI's own text into the page.
 */
export function svgPage(svg: string, nonce: string): string {
  // The nonce goes into an attribute and the policy: only hex is taken.
  if (!/^[0-9a-f]{16,64}$/.test(nonce)) throw new Error('svgPage: a nonce of hex digits is needed');
  const drawing = cleanSvg(svg);
  if (drawing === null) throw new Error('svgPage: not a drawing');
  const page = [
    'html,body{margin:0;padding:0;background:transparent;overflow:hidden}',
    'body{-webkit-text-size-adjust:100%;-webkit-tap-highlight-color:transparent}',
    '#m{background:#ffffff;border-radius:8px;overflow:hidden;padding:8px;box-sizing:border-box}',
    '#m>svg{display:block!important;position:static!important;max-width:100%!important;height:auto;margin:0 auto}',
  ].join('\n');
  return [
    '<!DOCTYPE html>',
    '<html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp(nonce)}">`,
    '<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">',
    `<style>\n${page}\n</style>`,
    '</head><body>',
    `<div id="m">${drawing}</div>`,
    `<script nonce="${nonce}">${script}</script>`,
    '</body></html>',
  ].join('\n');
}

/** What the page says: its height, a PNG of the picture, or that it could not make one. */
export type SvgMessage = { height: number } | { png: string; width: number; height: number } | { pngError: true };

const maxHeight = 3000;
// A PNG the page sends is base64 after this start, and a PNG's first eight
// bytes are always the same. A picture of 4096 px a side stays well under maxPng.
const pngStart = 'data:image/png;base64,';
const pngMagic = 'iVBORw0KGgo';
const maxPng = 24 * 1024 * 1024;
const side = (n: unknown): n is number => typeof n === 'number' && Number.isInteger(n) && n >= 1 && n <= 4096;

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
    const body = m.png.startsWith(pngStart) ? m.png.slice(pngStart.length) : '';
    const ok = body.startsWith(pngMagic) && /^[A-Za-z0-9+/]+={0,2}$/.test(body) && side(m.width) && side(m.height);
    return ok ? { png: body, width: m.width as number, height: m.height as number } : { pngError: true };
  }
  if (typeof m.height === 'number' && Number.isFinite(m.height) && m.height > 0) return { height: Math.min(Math.ceil(m.height), maxHeight) };
  return null;
}
