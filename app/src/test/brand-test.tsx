// S5c brand: app icon B (the user's 11-01 choice) as the launcher icon, the
// adaptive and themed icon layers and the icon-only native splash, all set
// in app.json; the PNGs scripts/render-brand.mjs drew (sizes, colour types,
// the board's colours at known points); the wordmark's measured box; and
// the About screen that carries the whole board F-Brand-Splash-Final.

import { act, render, userEvent } from '@testing-library/react-native';
import Constants from 'expo-constants';
import { router } from 'expo-router';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Platform, Pressable, Text, View } from 'react-native';

import RootLayout from '@/app/_layout';
import About from '@/app/about';
import { setLang } from '@/i18n';
import { LAUNCH_ICON, LaunchCover, SPLASH_WAIT, splashIconScale } from '@/ui/LaunchCover';
import { BrandMark, Wordmark } from '@/ui/Wordmark';
import box from '@/ui/wordmark-size.json';

const mockScheme = { value: 'light' as 'light' | 'dark' | null };
jest.mock('react-native/Libraries/Utilities/useColorScheme', () => ({ __esModule: true, default: () => mockScheme.value }));
jest.mock('expo-splash-screen', () => ({ preventAutoHideAsync: jest.fn(() => Promise.resolve(true)), hide: jest.fn() }));
const splash = require('expo-splash-screen') as { preventAutoHideAsync: jest.Mock; hide: jest.Mock };
// The launch cover holds the splash from its import on.
const heldAtImport = splash.preventAutoHideAsync.mock.calls.length > 0;

// The app has no Node types (tsconfig types: jest), so the few Node calls
// are typed here, as in a11y-static-test.
declare const __dirname: string;
const { existsSync, readFileSync }: { existsSync(p: string): boolean; readFileSync: { (p: string): Uint8Array; (p: string, enc: 'utf8'): string } } = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');
const { inflateSync }: { inflateSync(data: Uint8Array): Uint8Array } = require('node:zlib');

const appDir = join(__dirname, '..', '..');
const readJSON = (file: string) => JSON.parse(readFileSync(join(appDir, file), 'utf8'));

beforeEach(() => {
  mockScheme.value = 'light';
});

afterEach(() => setLang('en'));

// --- PNG ------------------------------------------------------------------

type PNG = { w: number; h: number; colour: number; at: (x: number, y: number) => [number, number, number, number] };

const u32 = (b: Uint8Array, at: number) => ((b[at] << 24) >>> 0) + (b[at + 1] << 16) + (b[at + 2] << 8) + b[at + 3];
const text = (b: Uint8Array, from: number, to: number) => String.fromCharCode(...b.subarray(from, to));

function head(file: string) {
  const b = readFileSync(join(appDir, file));
  expect([...b.subarray(0, 8)]).toEqual([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  expect(text(b, 12, 16)).toBe('IHDR');
  return { w: u32(b, 16), h: u32(b, 20), depth: b[24], colour: b[25], interlace: b[28] };
}

// png decodes an 8-bit, non-interlaced RGB or RGBA PNG.
function png(file: string): PNG {
  const b = readFileSync(join(appDir, file));
  const { w, h, colour } = head(file);
  const idat: Uint8Array[] = [];
  for (let at = 8; at < b.length; ) {
    const len = u32(b, at);
    if (text(b, at + 4, at + 8) === 'IDAT') idat.push(b.subarray(at + 8, at + 8 + len));
    at += 12 + len;
  }
  const joined = new Uint8Array(idat.reduce((n, d) => n + d.length, 0));
  idat.reduce((at, d) => (joined.set(d, at), at + d.length), 0);
  const bpp = colour === 6 ? 4 : 3;
  const raw = inflateSync(joined);
  const stride = w * bpp;
  const rows: Uint8Array[] = [];
  let prev = new Uint8Array(stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)];
    const line = raw.slice(y * (stride + 1) + 1, (y + 1) * (stride + 1));
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? line[x - bpp] : 0;
      const up = prev[x];
      const c = x >= bpp ? prev[x - bpp] : 0;
      const p = a + up - c;
      const paeth = Math.abs(p - a) <= Math.abs(p - up) && Math.abs(p - a) <= Math.abs(p - c) ? a : Math.abs(p - up) <= Math.abs(p - c) ? up : c;
      line[x] = (line[x] + [0, a, up, (a + up) >> 1, paeth][f]) & 0xff;
    }
    rows.push(line);
    prev = line;
  }
  return {
    w,
    h,
    colour,
    at: (x, y) => {
      const r = rows[y];
      return [r[x * bpp], r[x * bpp + 1], r[x * bpp + 2], bpp === 4 ? r[x * bpp + 3] : 255];
    },
  };
}

const hex = (p: number[]) => '#' + p.slice(0, 3).map((v) => v.toString(16).padStart(2, '0')).join('').toUpperCase() + (p[3] === 255 ? '' : `/${p[3]}`);

// at gives the pixel under a point of the board's art (viewBox units).
function board(img: PNG, viewBox: string, u: number, v: number) {
  const [vx, vy, vw] = viewBox.split(' ').map(Number);
  const k = img.w / vw;
  return hex(img.at(Math.floor((u - vx) * k), Math.floor((v - vy) * k)));
}

const APRICOT = '#FFAD85';
const INK = '#2A1408';
const icon = '0 0 100 100';
const adaptive = '-25 -25 150 150'; // the visible 72 dp of the 108 dp layer = the board's 100 units

// --- app.json -------------------------------------------------------------

test('app.json: icon B in apricot, the adaptive and themed layers, the icon-only splash', () => {
  const { expo } = readJSON('app.json');
  expect(expo.name).toBe('Asksemble');
  expect(expo.slug).toBe('asksemble');
  expect(`${expo.name} ${expo.slug}`).not.toMatch(/claude|anthropic/i);
  expect(expo.userInterfaceStyle).toBe('automatic');
  expect(expo.icon).toBe('./assets/icon.png');
  expect(expo.android.adaptiveIcon).toEqual({
    backgroundColor: '#FFAD85',
    foregroundImage: './assets/android-icon-foreground.png',
    backgroundImage: './assets/android-icon-background.png',
    monochromeImage: './assets/android-icon-monochrome.png',
  });
  // SDK 58 has no top-level native splash key: only the plugin sets it.
  expect(expo.splash).toBeUndefined();
  const splash = expo.plugins.filter((p: unknown) => p === 'expo-splash-screen' || (Array.isArray(p) && p[0] === 'expo-splash-screen'));
  expect(splash).toEqual([
    [
      'expo-splash-screen',
      {
        image: './assets/splash-icon.png',
        imageWidth: 112,
        resizeMode: 'contain',
        backgroundColor: '#FCFAF8',
        dark: { image: './assets/splash-icon.png', backgroundColor: '#151618' },
      },
    ],
  ]);
  for (const path of [expo.icon, ...Object.values(expo.android.adaptiveIcon).filter((v) => String(v).startsWith('./')), splash[0][1].image]) {
    expect(existsSync(join(appDir, String(path)))).toBe(true);
  }

  const { dependencies } = readJSON('package.json');
  expect(dependencies['expo-splash-screen']).toMatch(/^~58\./);
});

// --- PNGs -----------------------------------------------------------------

const RGB = 2;
const RGBA = 6;
const assets: [string, number, number, number][] = [
  ['assets/icon.png', 1024, 1024, RGB],
  ['assets/android-icon-background.png', 1024, 1024, RGB],
  ['assets/android-icon-foreground.png', 1024, 1024, RGBA],
  ['assets/android-icon-monochrome.png', 1024, 1024, RGBA],
  ['assets/splash-icon.png', 1024, 1024, RGBA],
  ['assets/favicon.png', 196, 196, RGBA],
  ['assets/brand/mark.png', 112, 112, RGBA],
  ['assets/brand/mark@2x.png', 224, 224, RGBA],
  ['assets/brand/mark@3x.png', 336, 336, RGBA],
  ...(['dark', 'light'] as const).flatMap((s) =>
    [1, 2, 3].map((k): [string, number, number, number] => [`assets/brand/wordmark-${s}${k === 1 ? '' : `@${k}x`}.png`, box.width * k, box.height * k, RGBA]),
  ),
];

test.each(assets)('%s is %i×%i, colour type %i, 8-bit, not interlaced', (file, w, h, colour) => {
  expect(head(file)).toEqual({ w, h, depth: 8, colour, interlace: 0 });
});

test('the store icon is full-bleed apricot with the ink bubble, and has no alpha', () => {
  const img = png('assets/icon.png');
  expect(img.colour).toBe(RGB);
  expect(board(img, icon, 0.2, 0.2)).toBe(APRICOT);
  expect(board(img, icon, 99.8, 99.8)).toBe(APRICOT);
  expect(board(img, icon, 50, 33)).toBe(INK); // top circle
  expect(board(img, icon, 29.5, 69.5)).toBe(INK); // tail
  expect(board(img, icon, 48.5, 51.5)).toBe(APRICOT); // the ring that parts the two lower circles
});

test('the adaptive layers: solid apricot behind, the mark inside the 66 dp safe circle, a themed silhouette with the rings cut out', () => {
  const back = png('assets/android-icon-background.png');
  for (let y = 0; y < back.h; y += 31) for (let x = 0; x < back.w; x += 31) expect(hex(back.at(x, y))).toBe(APRICOT);

  const fore = png('assets/android-icon-foreground.png');
  const mono = png('assets/android-icon-monochrome.png');
  expect(board(fore, adaptive, 50, 33)).toBe(INK);
  expect(board(fore, adaptive, 48.5, 51.5)).toBe(APRICOT);
  expect(board(fore, adaptive, -24, -24)).toMatch(/\/0$/);
  expect(board(mono, adaptive, 50, 33)).toBe('#FFFFFF');
  expect(board(mono, adaptive, 30, 55)).toBe('#FFFFFF');
  expect(board(mono, adaptive, 48.5, 51.5)).toMatch(/\/0$/);

  // Nothing drawn outside the 66 dp circle of the 108 dp layer; the mark is
  // as big as on the board (its ink reaches r = 32.5 of the board's 150).
  for (const img of [fore, mono]) {
    let far = 0;
    for (let y = 0; y < img.h; y += 2) {
      for (let x = 0; x < img.w; x += 2) {
        if (img.at(x, y)[3] > 0) far = Math.max(far, Math.hypot(x + 0.5 - img.w / 2, y + 0.5 - img.h / 2));
      }
    }
    expect(far).toBeLessThanOrEqual((33 / 108) * img.w);
    expect(far).toBeGreaterThan((30 / 150) * img.w);
  }
  for (let y = 0; y < mono.h; y += 7) {
    for (let x = 0; x < mono.w; x += 7) {
      const p = mono.at(x, y);
      if (p[3] > 64) expect(Math.min(p[0], p[1], p[2])).toBeGreaterThanOrEqual(249);
    }
  }
});

test.each(['assets/splash-icon.png', 'assets/favicon.png', 'assets/brand/mark.png', 'assets/brand/mark@3x.png'])('%s is the squircle icon on transparent', (file) => {
  const img = png(file);
  expect(board(img, icon, 1.5, 1.5)).toMatch(/\/0$/);
  expect(board(img, icon, 98.5, 98.5)).toMatch(/\/0$/);
  expect(board(img, icon, 6, 50)).toBe(APRICOT);
  expect(board(img, icon, 50, 33)).toBe(INK);
  expect(board(img, icon, 48.5, 51.5)).toBe(APRICOT);
});

test.each([
  ['dark', '#FFAD85', '#EDEBE7'],
  ['light', '#DD5A26', '#1D1A17'],
])('the %s wordmark is Ask %s + semble %s on transparent', (scheme, ask, semble) => {
  const img = png(`assets/brand/wordmark-${scheme}@2x.png`);
  const seen = new Map<string, number>();
  for (let y = 0; y < img.h; y++) {
    for (let x = 0; x < img.w; x++) {
      const p = img.at(x, y);
      if (p[3] === 255) seen.set(hex(p), (seen.get(hex(p)) ?? 0) + 1);
    }
  }
  expect([...seen.keys()].sort()).toEqual([ask, semble].sort());
  // 'Ask' is the left part: the first solid column is apricot.
  let first = '';
  for (let x = 0; x < img.w && !first; x++) for (let y = 0; y < img.h && !first; y++) if (img.at(x, y)[3] === 255) first = hex(img.at(x, y));
  expect(first).toBe(ask);
  expect(hex(img.at(0, 0))).toMatch(/\/0$/);
});

test('wordmark-size.json is the 1x wordmark box at 36 px', () => {
  expect(box.fontSize).toBe(36);
  for (const s of ['dark', 'light']) {
    const one = head(`assets/brand/wordmark-${s}.png`);
    expect([one.w, one.h]).toEqual([box.width, box.height]);
  }
  // Noto Sans KR 800 'Asksemble' at 36 px: about 5.2 em wide, 1.1 em tall.
  expect(box.width / box.fontSize).toBeGreaterThan(4.9);
  expect(box.width / box.fontSize).toBeLessThan(5.6);
  expect(box.height).toBe(Math.ceil(36 * 1.1));
});

// --- components -----------------------------------------------------------

const style = (el: { props: { style?: unknown } }) => Object.assign({}, ...[el.props.style].flat(3).filter(Boolean)) as Record<string, unknown>;

test('Wordmark scales its box with the font size and takes a scheme; BrandMark is decoration', async () => {
  const r = await render(
    <View>
      <Wordmark size={18} scheme="dark" />
      <BrandMark size={56} />
    </View>,
  );
  const word = r.getByRole('image', { name: 'Asksemble' });
  expect(word.props.testID).toBe('wordmark-dark');
  expect(style(word)).toMatchObject({ width: box.width / 2, height: box.height / 2 });
  expect(r.queryByTestId('brand-mark')).toBeNull(); // hidden from screen readers
  expect(style(r.getByTestId('brand-mark', { includeHiddenElements: true }))).toMatchObject({ width: 56, height: 56 });
});

// --- About ----------------------------------------------------------------

function Home() {
  return (
    <View>
      <Text>home screen</Text>
      <Pressable accessibilityRole="button" onPress={() => router.push('/about')}>
        <Text>About</Text>
      </Pressable>
    </View>
  );
}

const routes = { _layout: RootLayout, index: Home, about: About };

// Under jest the app config has no version; the phone's has app.json's.
const config = Constants.expoConfig as { version?: string } | null;
const appVersion: string = readJSON('app.json').expo.version;
let savedVersion: string | undefined;

beforeEach(() => {
  savedVersion = config?.version;
  if (config) config.version = appVersion;
});

afterEach(() => {
  if (config) config.version = savedVersion;
});

test('About shows the mark, the wordmark, the slogan, the description, the version and the not-official line', async () => {
  const user = userEvent.setup();
  const r = await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'About' }));
  await waitFor(() => expect(r.getPathname()).toBe('/about'));

  const word = await screen.findByRole('image', { name: 'Asksemble' });
  expect(word.props.testID).toBe('wordmark-light');
  expect(style(word)).toMatchObject({ width: box.width, height: box.height });
  expect(style(screen.getByTestId('brand-mark', { includeHiddenElements: true }))).toMatchObject({ width: 112, height: 112 });
  expect(screen.getByText('Ask, assemble.')).toBeTruthy();
  expect(screen.getByText("Ask every AI at once. See where they agree, and where they don't.")).toBeTruthy();
  expect(screen.getByText(`Version ${appVersion}`)).toBeTruthy();
  expect(screen.getByText('Not an official product of any AI company.')).toBeTruthy();
  expect(screen.queryByText(/Claude|Anthropic/)).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Back' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
  expect(await screen.findByText('home screen')).toBeTruthy();
});

test('About reads in Korean, and Back with nothing behind it goes to the start', async () => {
  setLang('ko');
  const user = userEvent.setup();
  const r = await renderRouter(routes, { initialUrl: '/about' });
  expect(await screen.findByRole('image', { name: 'Asksemble' })).toBeTruthy();
  expect(screen.getByText('Ask, assemble.')).toBeTruthy();
  expect(screen.getByText('한 번 물으면 여러 AI가 답하고, 같은 말과 다른 말을 한눈에 보여 줍니다.')).toBeTruthy();
  expect(screen.getByText(`버전 ${appVersion}`)).toBeTruthy();
  expect(screen.getByText('AI 회사의 공식 제품이 아닙니다.')).toBeTruthy();

  expect(router.canGoBack()).toBe(false);
  await user.press(screen.getByRole('button', { name: '뒤로' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
  expect(await screen.findByText('home screen')).toBeTruthy();
});

test('About draws the dark wordmark on the dark theme', async () => {
  mockScheme.value = 'dark';
  await renderRouter(routes, { initialUrl: '/about' });
  const word = await screen.findByRole('image', { name: 'Asksemble' });
  expect(word.props.testID).toBe('wordmark-dark');
});

test('About leaves the version line out when the app has no version', async () => {
  if (config) config.version = undefined;
  await renderRouter(routes, { initialUrl: '/about' });
  expect(await screen.findByText('Not an official product of any AI company.')).toBeTruthy();
  expect(screen.queryByText(/^Version/)).toBeNull();
});

// --- Launch cover (#1) ------------------------------------------------------

// R-60: the APK opened on the icon alone, while the board
// F-Brand-Splash-Final has the wordmark under it. The native splash can draw
// only the icon, so the cover takes over in the same place and colour as the
// splash, brings the wordmark in, then goes away.
test.each(['light', 'dark'] as const)('the launch cover matches the %s splash, adds the wordmark and goes away', async (scheme) => {
  mockScheme.value = scheme;
  jest.useFakeTimers();
  try {
    const splash = readJSON('app.json').expo.plugins.find((p: unknown) => Array.isArray(p) && p[0] === 'expo-splash-screen')[1];
    await render(<LaunchCover />);
    const hidden = { includeHiddenElements: true };
    const cover = screen.getByTestId('launch-cover', hidden);
    expect(style(cover).backgroundColor).toBe(scheme === 'dark' ? splash.dark.backgroundColor : splash.backgroundColor);
    expect(LAUNCH_ICON).toBe(splash.imageWidth);
    expect(style(screen.getByTestId('launch-mark', hidden))).toMatchObject({ width: LAUNCH_ICON, height: LAUNCH_ICON });
    expect(screen.getByTestId(`launch-wordmark-${scheme}`, hidden)).toBeTruthy();
    // Never in the way: no touches, not read out.
    expect(cover.props.pointerEvents).toBe('none');
    expect(screen.queryByTestId('launch-cover')).toBeNull();
    // It moves once its icon is drawn (R-67).
    await act(async () => screen.getByTestId('launch-mark', hidden).props.onLoad());
    await act(async () => {
      jest.advanceTimersByTime(3000);
    });
    expect(screen.queryByTestId('launch-cover', hidden)).toBeNull();
  } finally {
    jest.useRealTimers();
  }
});

// R-63: on the S10 (One UI, Android 12) the native splash drew the icon at
// half of imageWidth, so the icon jumped to twice its size as the cover
// came. The cover's icon starts at the splash's size and grows to the board's.
// R-69: Samsung on Android 13 and later (the S23) draws it whole, and a cover
// icon starting at half grew behind the splash's full one.
test('the launch icon starts at the size the native splash drew it', () => {
  expect(splashIconScale({ os: 'android', version: 31, manufacturer: 'samsung' })).toBe(0.5);
  expect(splashIconScale({ os: 'android', version: 32, manufacturer: 'Samsung' })).toBe(0.5);
  for (const version of [33, 34, 35, 36]) expect(splashIconScale({ os: 'android', version, manufacturer: 'samsung' })).toBe(1);
  expect(splashIconScale({ os: 'android', version: 34, manufacturer: 'Google' })).toBe(1);
  // Before Android 12 the splash library draws it, at imageWidth.
  expect(splashIconScale({ os: 'android', version: 30, manufacturer: 'samsung' })).toBe(1);
  expect(splashIconScale({ os: 'ios', version: '18.0' })).toBe(1);
});

// The grow runs on the native driver, which tests do not play; they see
// where it starts.
const growStart = () => (style(screen.getByTestId('launch-mark-grow', { includeHiddenElements: true })).transform as { scale: number }[])[0].scale;

test('on a Samsung phone with Android 12 the launch icon starts at half size', async () => {
  jest.replaceProperty(Platform, 'OS', 'android');
  jest.spyOn(Platform, 'Version', 'get').mockReturnValue(31);
  jest.spyOn(Platform, 'constants', 'get').mockReturnValue({ ...Platform.constants, Manufacturer: 'samsung' } as typeof Platform.constants);
  try {
    await render(<LaunchCover />);
    expect(growStart()).toBe(0.5);
  } finally {
    jest.restoreAllMocks();
  }
});

test('elsewhere the launch icon starts at full size', async () => {
  await render(<LaunchCover />);
  expect(growStart()).toBe(1);
});

test('R-69: on a Samsung phone with Android 13 or later the launch icon starts at full size', async () => {
  jest.replaceProperty(Platform, 'OS', 'android');
  jest.spyOn(Platform, 'Version', 'get').mockReturnValue(34);
  jest.spyOn(Platform, 'constants', 'get').mockReturnValue({ ...Platform.constants, Manufacturer: 'samsung' } as typeof Platform.constants);
  try {
    await render(<LaunchCover />);
    expect(growStart()).toBe(1);
  } finally {
    jest.restoreAllMocks();
  }
});

// R-67: the cover's icon is an image that comes a moment after the cover's
// first frame, and the S10 showed a frame with no icon between the splash
// and the cover. The splash stays until the icon is drawn (or a wait runs
// out), and only then does the cover move.
describe('the splash waits for the cover’s icon', () => {
  beforeEach(() => {
    splash.hide.mockClear();
    jest.useFakeTimers();
  });
  afterEach(() => jest.useRealTimers());

  test('it is held from the start and goes once the icon is drawn', async () => {
    expect(heldAtImport).toBe(true);
    await render(<LaunchCover />);
    await act(async () => {
      jest.advanceTimersByTime(200);
    });
    expect(splash.hide).not.toHaveBeenCalled();
    // (The cover takes no touches, so the load is called rather than fired.)
    await act(async () => screen.getByTestId('launch-mark', { includeHiddenElements: true }).props.onLoad());
    await act(async () => {
      jest.advanceTimersByTime(50);
    });
    expect(splash.hide).toHaveBeenCalledTimes(1);
  });

  test('an icon that never says it is drawn does not keep the splash', async () => {
    await render(<LaunchCover />);
    await act(async () => {
      jest.advanceTimersByTime(SPLASH_WAIT - 50);
    });
    expect(splash.hide).not.toHaveBeenCalled();
    await act(async () => {
      jest.advanceTimersByTime(100);
    });
    await act(async () => {
      jest.advanceTimersByTime(50);
    });
    expect(splash.hide).toHaveBeenCalledTimes(1);
  });
});

test('the root layout opens with the launch cover', async () => {
  await renderRouter(routes, { initialUrl: '/' });
  expect(await screen.findByTestId('launch-cover', { includeHiddenElements: true })).toBeTruthy();
});
