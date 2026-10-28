// Checks on app.json and eas.json that only show up in a store build, not in
// Expo Go (B2).
import app from '../app.json';
import eas from '../eas.json';
import pkg from '../package.json';

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
  for (const p of ['READ_EXTERNAL_STORAGE', 'WRITE_EXTERNAL_STORAGE', 'SYSTEM_ALERT_WINDOW', 'VIBRATE']) {
    expect(blocked).toContain(`android.permission.${p}`);
  }
  const camera = expo.plugins.find((p) => Array.isArray(p) && p[0] === 'expo-camera') as [string, { recordAudioAndroid: boolean }];
  expect(camera[1].recordAudioAndroid).toBe(false);
});

test('EAS keeps the version code and sends store builds to internal testing as a draft', () => {
  expect(eas.cli.appVersionSource).toBe('remote');
  expect(eas.build.preview.android.buildType).toBe('apk');
  expect(eas.build.production.android.buildType).toBe('app-bundle');
  expect(eas.build.production.android.autoIncrement).toBe('versionCode');
  expect(eas.submit.production.android).toEqual({ track: 'internal', releaseStatus: 'draft' });
});
