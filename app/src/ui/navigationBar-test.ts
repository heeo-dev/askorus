// R-82 (#17): the Android navigation bar follows the app's theme through the
// app's own native module (modules/system-bars): a light bar for the light
// theme, every change sent (the splash puts the bar back as it was at
// start-up), a failure told and not thrown, nothing off Android or without
// the module.

import { Platform } from 'react-native';

const mockSet = jest.fn((_light: boolean) => Promise.resolve());
let mockModule: unknown = null;
jest.mock('expo', () => ({ requireOptionalNativeModule: (name: string) => (name === 'AsksembleSystemBars' ? mockModule : null) }));

function load(): typeof import('./navigationBar') {
  let m: typeof import('./navigationBar') | undefined;
  jest.isolateModules(() => {
    m = require('./navigationBar');
  });
  return m!;
}

beforeEach(() => {
  mockSet.mockClear();
  mockModule = { setNavigationBarLight: (light: boolean) => mockSet(light) };
});

afterEach(() => jest.restoreAllMocks());

test('on Android every change sets the bar, the same one again too: light for the light theme', () => {
  jest.replaceProperty(Platform, 'OS', 'android');
  const { applyNavigationBar } = load();
  applyNavigationBar('dark');
  applyNavigationBar('dark');
  applyNavigationBar('light');
  expect(mockSet.mock.calls).toEqual([[false], [false], [true]]);
});

test('a failure is told, not thrown; without the module or off Android nothing happens', async () => {
  jest.replaceProperty(Platform, 'OS', 'android');
  const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  mockSet.mockImplementationOnce(() => Promise.reject(new Error('no window')));
  load().applyNavigationBar('light');
  await new Promise((r) => setTimeout(r, 0));
  expect(warn).toHaveBeenCalledWith('[navigation bar]', expect.any(Error));

  mockSet.mockClear();
  mockModule = null;
  expect(() => load().applyNavigationBar('dark')).not.toThrow();
  mockModule = {};
  expect(() => load().applyNavigationBar('dark')).not.toThrow();
  expect(mockSet).not.toHaveBeenCalled();

  mockModule = { setNavigationBarLight: (light: boolean) => mockSet(light) };
  jest.replaceProperty(Platform, 'OS', 'ios');
  load().applyNavigationBar('dark');
  expect(mockSet).not.toHaveBeenCalled();
});
