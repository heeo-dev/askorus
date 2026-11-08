// Draws the brand PNGs: app icon I2-A, the adaptive icon layers, the
// splash icon, the favicon, the mark and the wordmark used by the About
// screen and the launch cover. The art lives in scripts/brand/icon.html and
// wordmark.html (copied from the masters in docs/brand, picked on 11-08,
// #31); headless Microsoft Edge screenshots them once on the PC and the PNGs
// are committed, so the app needs no SVG library or font file (Expo Go).
//
//   assets/icon.png                     1024, full-bleed apricot square, no alpha (iOS and stores mask it)
//   assets/android-icon-background.png  1024, solid apricot, no alpha
//   assets/android-icon-foreground.png  1024, the mark on transparent (adaptive layer, 108 dp)
//   assets/android-icon-monochrome.png  1024, the mark as a white silhouette (themed icons)
//   assets/splash-icon.png              1024, the squircle icon on transparent (native splash)
//   assets/favicon.png                  196, the squircle icon
//   assets/brand/mark[@2x|@3x].png      112 / 224 / 336, the squircle icon (About)
//   assets/brand/wordmark-{dark|light}[@2x|@3x].png and src/ui/wordmark-size.json
//
// Every PNG is checked (size, colour type, colours at known points of the
// board's art, transparent corners, nothing outside the safe zone) before
// any file in assets is replaced; a failed check stops with exit code 1.
//
// Run: node scripts/render-brand.mjs   (EDGE overrides the Edge path. The
// wordmark is outlines, so no font file is needed.)

import { execFile } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { crc32, deflateSync, inflateSync } from 'node:zlib';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const templates = join(app, 'scripts', 'brand');
const edge = process.env.EDGE || 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const parallel = 4;
// Edge does not draw a window smaller than this; smaller PNGs are cut out
// of a larger screenshot.
const minWindow = 128;

const apricot = [0xff, 0xad, 0x85];
const ink = [0x2a, 0x14, 0x08];
const white = [0xff, 0xff, 0xff];

// An adaptive icon layer is 108 dp; a launcher shows its middle 72 dp
// (circle, squircle or rounded square) and keeps a 66 dp circle whole. The
// board draws the icon as a 100-unit squircle with the mark inside its
// r=33 circle, and its 96 px and 48 px launcher mock-ups show the mark at
// 60% of the icon. Mapping the board's 100 units onto the visible 72 dp
// (108 dp = 150 units) keeps that look: the mark's ink reaches r=32.5
// units = 23.4 dp, well inside the 33 dp safe circle. (The S5c plan said
// '-4 -4 108 108', which puts the mark at 83% of the visible icon, larger
// than the board and close to a circle mask's edge.)
const adaptiveViewBox = '-25 -25 150 150';
const iconViewBox = '0 0 100 100';
const safeRadiusDp = 33;

const layers = {
  square: '<use href="#square"></use><use href="#mark"></use>',
  squircle: '<use href="#squircle"></use><use href="#mark"></use>',
  background: '<rect x="-100" y="-100" width="300" height="300" fill="#FFAD85"></rect>',
  foreground: '<use href="#mark"></use>',
  monochrome: '<rect x="-100" y="-100" width="300" height="300" fill="#FFFFFF" mask="url(#silhouette)"></rect>',
};

// Points of the master's art (viewBox units) and what must be there.
const markInk = [
  [42, 35, 'ink'], // top circle, beside its mouth
  [25, 55, 'ink'], // left circle, beside its mouth
  [75, 55, 'ink'], // right circle, beside its mouth
  [30, 70, 'ink'], // the tail, outside the left circle's ring
];
const ring = [48, 55]; // the right circle's ring, where it parts the left circle
const mouths = [[50, 33.5], [36, 56], [64, 56]]; // apricot, or cut out in the silhouette
const squircleChecks = [...markInk, [...ring, 'apricot'], ...mouths.map(([u, v]) => [u, v, 'apricot']), [50, 88, 'apricot'], [6, 50, 'apricot'], [1.5, 1.5, 'clear'], [98.5, 98.5, 'clear']];

const icons = [
  { file: 'icon.png', px: 1024, viewBox: iconViewBox, layers: layers.square, opaque: true, checks: [...markInk, [...ring, 'apricot'], ...mouths.map(([u, v]) => [u, v, 'apricot']), [0.2, 0.2, 'apricot'], [99.8, 99.8, 'apricot']] },
  { file: 'android-icon-background.png', px: 1024, viewBox: adaptiveViewBox, layers: layers.background, opaque: true, only: apricot },
  { file: 'android-icon-foreground.png', px: 1024, viewBox: adaptiveViewBox, layers: layers.foreground, safe: true, checks: [...markInk, [...ring, 'apricot'], ...mouths.map(([u, v]) => [u, v, 'apricot']), [50, 88, 'clear'], [-24, -24, 'clear'], [124, 124, 'clear']] },
  { file: 'android-icon-monochrome.png', px: 1024, viewBox: adaptiveViewBox, layers: layers.monochrome, safe: true, only: white, checks: [...markInk.map(([u, v]) => [u, v, 'white']), [...ring, 'clear'], ...mouths.map(([u, v]) => [u, v, 'clear']), [50, 88, 'clear'], [-24, -24, 'clear']] },
  { file: 'splash-icon.png', px: 1024, viewBox: iconViewBox, layers: layers.squircle, checks: squircleChecks },
  { file: 'favicon.png', px: 196, viewBox: iconViewBox, layers: layers.squircle, checks: squircleChecks },
  { file: 'brand/mark.png', px: 112, viewBox: iconViewBox, layers: layers.squircle, checks: squircleChecks },
  { file: 'brand/mark@2x.png', px: 224, viewBox: iconViewBox, layers: layers.squircle, checks: squircleChecks },
  { file: 'brand/mark@3x.png', px: 336, viewBox: iconViewBox, layers: layers.squircle, checks: squircleChecks },
];

// The wordmark: the outlines were made at 120 units per em, so a 36 px
// wordmark (as the old text one) is 0.3 px per unit. The box is the ink
// (A's left edge to the last s, the k's top to the o's tail) plus 4 px all
// round at 1x. Dark and light.
const word = {
  fontSize: 36,
  unitsPerEm: 120,
  ink: { x0: 20, y0: 21.28, x1: 493.6, y1: 127.3 }, // in wordmark.html's viewBox units
  pad: 4, // px at 1x
  margin: 48, // around the drawing in the screenshot, per 1x
  hole: [271.28, 83.2], // the middle of the o: see-through
  tail: [256, 121], // inside the o's tail, outside its circle
  schemes: {
    dark: { ask: '#FFAD85', rest: '#EDEBE7' },
    light: { ask: '#DD5A26', rest: '#1D1A17' },
  },
};

function fail(msg) {
  console.error(`render-brand: ${msg}`);
  process.exit(1);
}

// --- PNG ---------------------------------------------------------------

// decode reads an 8-bit RGB or RGBA PNG (no interlace) into RGBA.
function decode(buf) {
  if (buf.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a') throw new Error('not a PNG');
  let at = 8;
  let w = 0;
  let h = 0;
  let colour = 0;
  const idat = [];
  while (at < buf.length) {
    const len = buf.readUInt32BE(at);
    const type = buf.toString('latin1', at + 4, at + 8);
    const data = buf.subarray(at + 8, at + 8 + len);
    if (type === 'IHDR') {
      w = data.readUInt32BE(0);
      h = data.readUInt32BE(4);
      colour = data[9];
      if (data[8] !== 8 || (colour !== 2 && colour !== 6)) throw new Error(`bit depth ${data[8]}, colour type ${colour}; want 8-bit RGB or RGBA`);
      if (data[12] !== 0) throw new Error('interlaced');
    } else if (type === 'IDAT') idat.push(data);
    at += 12 + len;
  }
  const bpp = colour === 6 ? 4 : 3;
  const raw = inflateSync(Buffer.concat(idat));
  const stride = w * bpp;
  const rgba = Buffer.alloc(w * h * 4);
  let prev = Buffer.alloc(stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)];
    const line = Buffer.from(raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1)));
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? line[x - bpp] : 0;
      const b = prev[x];
      const c = x >= bpp ? prev[x - bpp] : 0;
      let add = 0;
      if (f === 1) add = a;
      else if (f === 2) add = b;
      else if (f === 3) add = (a + b) >> 1;
      else if (f === 4) add = paeth(a, b, c);
      else if (f !== 0) throw new Error(`bad filter ${f}`);
      line[x] = (line[x] + add) & 0xff;
    }
    for (let x = 0; x < w; x++) {
      const o = (y * w + x) * 4;
      rgba[o] = line[x * bpp];
      rgba[o + 1] = line[x * bpp + 1];
      rgba[o + 2] = line[x * bpp + 2];
      rgba[o + 3] = bpp === 4 ? line[x * bpp + 3] : 255;
    }
    prev = line;
  }
  return { w, h, rgba };
}

function paeth(a, b, c) {
  const p = a + b - c;
  const pa = Math.abs(p - a);
  const pb = Math.abs(p - b);
  const pc = Math.abs(p - c);
  return pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
}

function chunk(type, data) {
  const head = Buffer.alloc(8);
  head.writeUInt32BE(data.length, 0);
  head.write(type, 4, 'latin1');
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])) >>> 0, 0);
  return Buffer.concat([head, data, crc]);
}

// encode writes img as an 8-bit PNG: RGB (colour type 2) when opaque,
// else RGBA (6), with a filter per row and nothing but IHDR/IDAT/IEND.
function encode(img, opaque) {
  const bpp = opaque ? 3 : 4;
  const stride = img.w * bpp;
  const out = Buffer.alloc((stride + 1) * img.h);
  let prev = Buffer.alloc(stride);
  for (let y = 0; y < img.h; y++) {
    const line = Buffer.alloc(stride);
    for (let x = 0; x < img.w; x++) {
      const o = (y * img.w + x) * 4;
      for (let k = 0; k < bpp; k++) line[x * bpp + k] = img.rgba[o + k];
    }
    let best = null;
    let bestScore = Infinity;
    for (let f = 0; f <= 4; f++) {
      const row = Buffer.alloc(stride + 1);
      row[0] = f;
      let score = 0;
      for (let x = 0; x < stride; x++) {
        const a = x >= bpp ? line[x - bpp] : 0;
        const b = prev[x];
        const c = x >= bpp ? prev[x - bpp] : 0;
        const pred = f === 0 ? 0 : f === 1 ? a : f === 2 ? b : f === 3 ? (a + b) >> 1 : paeth(a, b, c);
        const v = (line[x] - pred) & 0xff;
        row[x + 1] = v;
        score += v < 128 ? v : 256 - v;
      }
      if (score < bestScore) {
        bestScore = score;
        best = row;
      }
    }
    best.copy(out, y * (stride + 1));
    prev = line;
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(img.w, 0);
  ihdr.writeUInt32BE(img.h, 4);
  ihdr[8] = 8;
  ihdr[9] = opaque ? 2 : 6;
  return Buffer.concat([
    Buffer.from('89504e470d0a1a0a', 'hex'),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(out, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

function crop(img, x, y, w, h) {
  if (x < 0 || y < 0 || x + w > img.w || y + h > img.h) throw new Error(`crop ${x},${y} ${w}x${h} is outside the ${img.w}x${img.h} screenshot`);
  const rgba = Buffer.alloc(w * h * 4);
  for (let r = 0; r < h; r++) img.rgba.copy(rgba, r * w * 4, ((y + r) * img.w + x) * 4, ((y + r) * img.w + x + w) * 4);
  return { w, h, rgba };
}

function px(img, x, y) {
  const o = (y * img.w + x) * 4;
  return [img.rgba[o], img.rgba[o + 1], img.rgba[o + 2], img.rgba[o + 3]];
}

const near = (p, c, tol = 2) => Math.abs(p[0] - c[0]) <= tol && Math.abs(p[1] - c[1]) <= tol && Math.abs(p[2] - c[2]) <= tol;
const hex = (p) => '#' + p.slice(0, 3).map((v) => v.toString(16).padStart(2, '0')).join('').toUpperCase() + (p[3] === 255 ? '' : `/${p[3]}`);
const rgbOf = (s) => [1, 3, 5].map((i) => parseInt(s.slice(i, i + 2), 16));

// inkBox is the box of every pixel that is not fully transparent.
function inkBox(img) {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -1;
  let y1 = -1;
  for (let y = 0; y < img.h; y++) {
    for (let x = 0; x < img.w; x++) {
      if (img.rgba[(y * img.w + x) * 4 + 3] === 0) continue;
      if (x < x0) x0 = x;
      if (y < y0) y0 = y;
      if (x > x1) x1 = x;
      if (y > y1) y1 = y;
    }
  }
  return x1 < 0 ? null : { x0, y0, x1, y1 };
}

// --- Edge --------------------------------------------------------------

// edgeRun starts Edge once, with a profile folder of its own that is
// removed after. mode 'shot' writes a PNG, 'dom' returns the DOM.
function edgeRun(html, w, h, mode, png) {
  const dir = mkdtempSync(join(tmpdir(), 'askorus-brand-'));
  const file = join(dir, 'page.html');
  writeFileSync(file, html);
  const args = [
    '--headless=new',
    '--disable-gpu',
    '--hide-scrollbars',
    '--no-first-run',
    `--user-data-dir=${join(dir, 'profile')}`,
    '--default-background-color=00000000',
    '--force-device-scale-factor=1',
    '--force-color-profile=srgb',
    `--window-size=${Math.max(w, minWindow)},${Math.max(h, minWindow)}`,
    '--virtual-time-budget=3000',
    mode === 'shot' ? `--screenshot=${png}` : '--dump-dom',
    pathToFileURL(file).href,
  ];
  return new Promise((resolve, reject) => {
    execFile(edge, args, { timeout: 90000, windowsHide: true, maxBuffer: 64 << 20 }, (err, stdout) => {
      rmSync(dir, { recursive: true, force: true });
      if (mode === 'shot') {
        if (!existsSync(png)) reject(err ?? new Error('Edge wrote no screenshot'));
        else resolve();
      } else if (err && !stdout) reject(err);
      else resolve(stdout);
    });
  });
}

async function shot(html, w, h, tmp, name) {
  const png = join(tmp, `${name.replace(/[\\/]/g, '_')}-raw.png`);
  await edgeRun(html, w, h, 'shot', png);
  const img = decode(readFileSync(png));
  if (img.w !== Math.max(w, minWindow) || img.h !== Math.max(h, minWindow)) throw new Error(`screenshot is ${img.w}x${img.h}, want the window ${Math.max(w, minWindow)}x${Math.max(h, minWindow)}`);
  return img;
}

// template reads scripts/brand/<name> without its comments (they name the
// {{slots}} too).
function template(name) {
  return readFileSync(join(templates, name), 'utf8').replace(/<!--[\s\S]*?-->/g, '');
}

function fill(text, values) {
  return text.replace(/\{\{(\w+)\}\}/g, (m, k) => {
    if (!(k in values)) throw new Error(`template needs {{${k}}}`);
    return String(values[k]);
  });
}

async function pool(jobs) {
  const queue = [...jobs];
  const errors = [];
  await Promise.all(
    Array.from({ length: Math.min(parallel, queue.length) }, async () => {
      while (queue.length) {
        const job = queue.shift();
        try {
          await job();
        } catch (e) {
          errors.push(e instanceof Error ? e.message : String(e));
        }
      }
    }),
  );
  if (errors.length) fail(errors.join('\n'));
}

// --- icons -------------------------------------------------------------

function checkIcon(spec, img) {
  const [vx, vy, vw] = spec.viewBox.split(' ').map(Number);
  const scale = spec.px / vw;
  const at = (u, v) => [Math.floor((u - vx) * scale), Math.floor((v - vy) * scale)];
  const where = `${spec.file}`;
  for (const [u, v, want] of spec.checks ?? []) {
    const [x, y] = at(u, v);
    const p = px(img, x, y);
    const ok =
      want === 'clear' ? p[3] === 0 : p[3] === 255 && near(p, want === 'ink' ? ink : want === 'apricot' ? apricot : white);
    if (!ok) throw new Error(`${where}: at board (${u}, ${v}) = pixel (${x}, ${y}) want ${want}, got ${hex(p)}`);
  }
  let seeThrough = 0;
  for (let i = 0; i < img.w * img.h; i++) {
    const p = [img.rgba[i * 4], img.rgba[i * 4 + 1], img.rgba[i * 4 + 2], img.rgba[i * 4 + 3]];
    if (p[3] < 255) seeThrough++;
    if (spec.only && p[3] > 64 && !near(p, spec.only, 6)) throw new Error(`${where}: pixel ${i % img.w},${Math.floor(i / img.w)} is ${hex(p)}, want only ${hex([...spec.only, 255])}`);
  }
  if (spec.opaque && seeThrough) throw new Error(`${where}: ${seeThrough} pixels are not opaque`);
  if (spec.safe) {
    // Nothing may lie outside the 66 dp circle of the 108 dp layer.
    const c = spec.px / 2;
    const r = (safeRadiusDp / 108) * spec.px;
    for (let y = 0; y < img.h; y++) {
      for (let x = 0; x < img.w; x++) {
        if (img.rgba[(y * img.w + x) * 4 + 3] === 0) continue;
        if (Math.hypot(x + 0.5 - c, y + 0.5 - c) > r) throw new Error(`${where}: pixel ${x},${y} lies outside the safe circle`);
      }
    }
  }
}

async function renderIcons(tmp, results) {
  const page = template('icon.html');
  await pool(
    icons.map((spec) => async () => {
      const html = fill(page, { px: spec.px, viewBox: spec.viewBox, layers: spec.layers });
      const img = crop(await shot(html, spec.px, spec.px, tmp, spec.file), 0, 0, spec.px, spec.px);
      checkIcon(spec, img);
      results.push({ file: join('assets', spec.file), png: encode(img, !!spec.opaque), img, opaque: !!spec.opaque });
    }),
  );
}

// --- wordmark ----------------------------------------------------------

async function renderWordmark(tmp, results) {
  const html = template('wordmark.html');
  const unit = word.fontSize / word.unitsPerEm; // px per unit at 1x
  const padU = word.pad / unit;
  const vx = word.ink.x0 - padU;
  const vy = word.ink.y0 - padU;
  const W = Math.ceil((word.ink.x1 - word.ink.x0) * unit + 2 * word.pad);
  const H = Math.ceil((word.ink.y1 - word.ink.y0) * unit + 2 * word.pad);
  const viewBox = `${vx} ${vy} ${W / unit} ${H / unit}`;
  const at = (k, [u, v]) => [Math.floor((u - vx) * unit * k), Math.floor((v - vy) * unit * k)];

  await pool(
    Object.entries(word.schemes).flatMap(([scheme, colours]) =>
      [1, 2, 3].map((k) => async () => {
        const name = `wordmark-${scheme}${k === 1 ? '' : `@${k}x`}.png`;
        const m = word.margin * k;
        const w = W * k;
        const h = H * k;
        const img = await shot(fill(html, { w, h, viewBox, ask: colours.ask, rest: colours.rest, left: m, top: m }), w + 2 * m, h + 2 * m, tmp, name);
        // Nothing may be drawn outside the box.
        const b = inkBox(img);
        if (!b) throw new Error(`${name}: nothing drawn`);
        if (b.x0 < m || b.y0 < m || b.x1 >= m + w || b.y1 >= m + h) throw new Error(`${name}: ink ${JSON.stringify(b)} lies outside the box ${m},${m} ${w}x${h}`);
        const cut = crop(img, m, m, w, h);
        // The pad is clear on every side: the ink box is the master's.
        const c = inkBox(cut);
        const padPx = Math.floor(word.pad * k) - 1;
        if (c.x0 < padPx || c.y0 < padPx || c.x1 > w - 1 - padPx || c.y1 > h - 1 - padPx) throw new Error(`${name}: ink ${JSON.stringify(c)} reaches into the ${word.pad * k} px pad of ${w}x${h}`);
        // Solid pixels are the two colours only, and both are there.
        const ask = rgbOf(colours.ask);
        const rest = rgbOf(colours.rest);
        let nAsk = 0;
        let nRest = 0;
        for (let i = 0; i < cut.w * cut.h; i++) {
          const p = [cut.rgba[i * 4], cut.rgba[i * 4 + 1], cut.rgba[i * 4 + 2], cut.rgba[i * 4 + 3]];
          if (p[3] !== 255) continue;
          if (near(p, ask)) nAsk++;
          else if (near(p, rest)) nRest++;
          else throw new Error(`${name}: solid pixel ${hex(p)} is neither ${colours.ask} nor ${colours.rest}`);
        }
        if (nAsk < 20 * k * k || nRest < 40 * k * k) throw new Error(`${name}: too few solid pixels (Ask ${nAsk}, orus ${nRest})`);
        // The o is a ring with a tail: clear in the middle, ink in the tail.
        const [hx, hy] = at(k, word.hole);
        if (px(cut, hx, hy)[3] !== 0) throw new Error(`${name}: the o is not see-through at its middle (${hx}, ${hy})`);
        const [tx, ty] = at(k, word.tail);
        if (px(cut, tx, ty)[3] < 128) throw new Error(`${name}: no ink in the o's tail at (${tx}, ${ty})`);
        results.push({ file: join('assets', 'brand', name), png: encode(cut, false), img: cut, opaque: false });
      }),
    ),
  );

  const json = { fontSize: word.fontSize, width: W, height: H };
  results.push({ file: join('src', 'ui', 'wordmark-size.json'), text: JSON.stringify(json, null, 2) + '\n' });
  return json;
}

// --- main --------------------------------------------------------------

if (!existsSync(edge)) fail(`Edge not found at ${edge} (set EDGE)`);
const tmp = mkdtempSync(join(tmpdir(), 'askorus-brand-out-'));
try {
  const results = [];
  await renderIcons(tmp, results);
  let wm;
  try {
    wm = await renderWordmark(tmp, results);
  } catch (e) {
    fail(e instanceof Error ? e.message : String(e));
  }
  // Every check passed: replace the files.
  mkdirSync(join(app, 'assets', 'brand'), { recursive: true });
  for (const r of results.sort((a, b) => a.file.localeCompare(b.file))) {
    const to = join(app, r.file);
    if (r.text !== undefined) {
      writeFileSync(to, r.text);
      console.log(`render-brand: ${r.file.replace(/\\/g, '/')} ${r.text.trim().replace(/\s+/g, ' ')}`);
    } else {
      writeFileSync(join(tmp, 'out.png'), r.png);
      copyFileSync(join(tmp, 'out.png'), to);
      console.log(`render-brand: ${r.file.replace(/\\/g, '/')} ${r.img.w}x${r.img.h} ${r.opaque ? 'RGB' : 'RGBA'} ${r.png.length} bytes`);
    }
  }
  console.log(`render-brand: wordmark box ${wm.width}x${wm.height} at ${word.fontSize} px`);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
