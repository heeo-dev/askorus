// Draws the app's icons (src/ui/Icon.tsx) into assets/icons/<name>.png from
// scripts/icons.json, whose SVG markup is copied from the design boards.
// Each icon is a 96×96 PNG, white strokes on transparent: the app tints it
// with the theme colour (Image tintColor), so no SVG library is needed and
// it runs in Expo Go. The drawing is done once on the PC by headless
// Microsoft Edge; the PNGs are committed.
//
// Run it after changing icons.json: node scripts/render-icons.mjs [name…]
// (no names: every icon). Set EDGE to use another Edge or Chrome.

import { execFile } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { inflateSync } from 'node:zlib';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const out = join(app, 'assets', 'icons');
const edge = process.env.EDGE || 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const size = 96;
const parallel = 4;

function fail(msg) {
  console.error(`render-icons: ${msg}`);
  process.exit(1);
}

const icons = JSON.parse(readFileSync(join(app, 'scripts', 'icons.json'), 'utf8'));
const wanted = process.argv.slice(2);
for (const n of wanted) if (!icons[n]) fail(`no icon named ${n} in icons.json`);
const names = wanted.length ? wanted : Object.keys(icons);
if (!existsSync(edge)) fail(`Edge not found at ${edge} (set EDGE)`);
mkdirSync(out, { recursive: true });

function page(icon) {
  if (typeof icon.svg !== 'string' || typeof icon.strokeWidth !== 'number') fail('each icon needs svg (string) and strokeWidth (number)');
  return (
    '<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:transparent}svg{display:block}</style></head><body>' +
    `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="#FFFFFF"` +
    ` stroke-width="${icon.strokeWidth}" stroke-linecap="round" stroke-linejoin="round">${icon.svg}</svg></body></html>`
  );
}

// run starts Edge once, with a profile folder of its own that is removed after.
function run(html, png) {
  const dir = mkdtempSync(join(tmpdir(), 'askorus-icon-'));
  const file = join(dir, 'icon.html');
  writeFileSync(file, html);
  const args = [
    '--headless=new',
    '--disable-gpu',
    '--hide-scrollbars',
    '--no-first-run',
    `--user-data-dir=${join(dir, 'profile')}`,
    '--default-background-color=00000000',
    '--force-device-scale-factor=1',
    `--window-size=${size},${size}`,
    `--screenshot=${png}`,
    pathToFileURL(file).href,
  ];
  return new Promise((resolve, reject) => {
    execFile(edge, args, { timeout: 60000, windowsHide: true }, (err) => {
      rmSync(dir, { recursive: true, force: true });
      if (err && !existsSync(png)) reject(err);
      else resolve();
    });
  });
}

// pixels reads an 8-bit RGBA PNG (no interlace) and gives its rows.
function pixels(buf) {
  const sig = '89504e470d0a1a0a';
  if (buf.subarray(0, 8).toString('hex') !== sig) throw new Error('not a PNG');
  let at = 8;
  let w = 0;
  let h = 0;
  const idat = [];
  while (at < buf.length) {
    const len = buf.readUInt32BE(at);
    const type = buf.toString('latin1', at + 4, at + 8);
    const data = buf.subarray(at + 8, at + 8 + len);
    if (type === 'IHDR') {
      w = data.readUInt32BE(0);
      h = data.readUInt32BE(4);
      const depth = data[8];
      const colour = data[9];
      const interlace = data[12];
      if (w !== size || h !== size) throw new Error(`size ${w}×${h}, want ${size}×${size}`);
      if (depth !== 8 || colour !== 6) throw new Error(`bit depth ${depth}, colour type ${colour}; want 8-bit RGBA (6)`);
      if (interlace !== 0) throw new Error('interlaced');
    } else if (type === 'IDAT') idat.push(data);
    at += 12 + len;
  }
  const raw = inflateSync(Buffer.concat(idat));
  const stride = w * 4;
  const rows = [];
  let prev = Buffer.alloc(stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)];
    const line = Buffer.from(raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1)));
    for (let x = 0; x < stride; x++) {
      const a = x >= 4 ? line[x - 4] : 0;
      const b = prev[x];
      const c = x >= 4 ? prev[x - 4] : 0;
      let add = 0;
      if (f === 1) add = a;
      else if (f === 2) add = b;
      else if (f === 3) add = (a + b) >> 1;
      else if (f === 4) {
        const p = a + b - c;
        const pa = Math.abs(p - a);
        const pb = Math.abs(p - b);
        const pc = Math.abs(p - c);
        add = pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
      } else if (f !== 0) throw new Error(`bad filter ${f}`);
      line[x] = (line[x] + add) & 0xff;
    }
    rows.push(line);
    prev = line;
  }
  return rows;
}

// check makes sure the icon came out: something drawn, white, on transparent.
function check(png) {
  const rows = pixels(readFileSync(png));
  let drawn = 0;
  let dark = 0;
  for (const line of rows) {
    for (let x = 0; x < line.length; x += 4) {
      if (line[x + 3] === 0) continue;
      drawn++;
      if (line[x + 3] > 64 && Math.min(line[x], line[x + 1], line[x + 2]) < 200) dark++;
    }
  }
  if (drawn < 40) throw new Error(`only ${drawn} pixels drawn`);
  if (dark > 0) throw new Error(`${dark} pixels are not white`);
  if (drawn > size * size * 0.6) throw new Error('the background is not transparent');
}

async function one(name) {
  const tmp = mkdtempSync(join(tmpdir(), 'askorus-png-'));
  const png = join(tmp, `${name}.png`);
  try {
    await run(page(icons[name]), png);
    check(png);
    copyFileSync(png, join(out, `${name}.png`));
    console.log(`render-icons: ${name}.png`);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

const queue = [...names];
let failed = 0;
await Promise.all(
  Array.from({ length: Math.min(parallel, queue.length) }, async () => {
    while (queue.length) {
      const name = queue.shift();
      try {
        await one(name);
      } catch (e) {
        failed++;
        console.error(`render-icons: ${name}: ${e instanceof Error ? e.message : e}`);
      }
    }
  }),
);
if (failed) fail(`${failed} of ${names.length} icons failed`);
console.log(`render-icons: ${names.length} icons in assets/icons`);
