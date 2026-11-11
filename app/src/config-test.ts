// Checks on app.json and eas.json that only show up in a store build, not in
// Expo Go (B2).
import app from '../app.json';
import eas from '../eas.json';
import pkg from '../package.json';

// Node's own modules, typed here: the app has no Node types.
declare const __dirname: string;
type Entry = { name: string; isDirectory(): boolean };
const { readdirSync, readFileSync }: {
  readdirSync(p: string, o: { withFileTypes: true }): Entry[];
  readFileSync(p: string, enc: 'utf8'): string;
} = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');

const expo = app.expo;

test('the Android package is set and never names Claude or Anthropic', () => {
  expect(expo.android.package).toMatch(/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$/);
  expect(`${expo.name} ${expo.slug} ${expo.android.package}`).not.toMatch(/claude|anthropic/i);
});

// R-14: without expo-system-ui a store build ignores "automatic" and always
// shows the light theme; Expo Go hid this.
test('following the phone dark mode has expo-system-ui', () => {
  expect(expo.userInterfaceStyle).toBe('automatic');
  expect(pkg.dependencies).toHaveProperty('expo-system-ui');
});

test('the build asks only for the camera and the network', () => {
  const blocked = expo.android.blockedPermissions;
  for (const p of ['READ_EXTERNAL_STORAGE', 'WRITE_EXTERNAL_STORAGE', 'SYSTEM_ALERT_WINDOW', 'VIBRATE', 'USE_BIOMETRIC', 'USE_FINGERPRINT']) {
    expect(blocked).toContain(`android.permission.${p}`);
  }
  expect(expo.android.permissions).toEqual(['android.permission.CAMERA']);
  expect(expo.ios.infoPlist.NSCameraUsageDescription).toMatch(/QR code/);
});

// #32: expo-camera read QR codes with Google ML Kit, which sends diagnostics
// and usage numbers to Google. The pairing QR is read by the app's own module
// now (modules/qr-scanner: CameraX and ZXing, all on the phone). Putting a
// camera or barcode library back means checking what it sends first; the
// Android libraries that end up in the build are checked in licenses-test.
test('no camera library that reads codes with ML Kit', () => {
  for (const name of ['expo-camera', 'expo-barcode-scanner', 'react-native-vision-camera']) {
    expect(pkg.dependencies).not.toHaveProperty(name);
  }
  const plugins = expo.plugins.map((p) => (Array.isArray(p) ? p[0] : p));
  expect(plugins).not.toContain('expo-camera');
});

// #32: expo-secure-store brings androidx.biometric, whose manifest adds the two
// biometric permissions. The library touches biometrics only for an item saved
// with requireAuthentication, which the app never asks for, so the permissions
// are blocked. Whoever starts using that option has to unblock them first.
test('nothing saved in the secure store asks for biometric authentication', () => {
  const sources = (dir: string): string[] =>
    readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
      const p = join(dir, e.name);
      if (e.isDirectory()) return sources(p);
      return /\.tsx?$/.test(e.name) && !/-test\.tsx?$/.test(e.name) ? [p] : [];
    });
  const files = sources(__dirname);
  expect(files.length).toBeGreaterThan(20);
  const using = files.filter((f) => /requireAuthentication|expo-local-authentication/.test(readFileSync(f, 'utf8')));
  expect(using).toEqual([]);
});

test('EAS keeps the version code and sends store builds to internal testing as a draft', () => {
  expect(eas.cli.appVersionSource).toBe('remote');
  expect(eas.build.preview.android.buildType).toBe('apk');
  expect(eas.build.production.android.buildType).toBe('app-bundle');
  expect(eas.build.production.android.autoIncrement).toBe('versionCode');
  expect(eas.submit.production.android).toEqual({ track: 'internal', releaseStatus: 'draft' });
});
