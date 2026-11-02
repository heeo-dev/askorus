// R-62: on the S10 (Android 12, Expo Go) the window shrinks for the
// keyboard, and a bar lifted by the keyboard's height as well pushed the
// new-session form out of sight. The lift is how far the keyboard reaches
// over the app's root view.

import { keyboardOver } from './keyboard';

const s10 = { keyboardTop: 491, height: 329.5, navBar: 48, android: true };

test('a window that does not shrink: the keyboard covers its height of the app', () => {
  expect(keyboardOver({ ...s10, root: 820.5 })).toBe(330);
});

test('a window that shrank to end at the keyboard: nothing to lift', () => {
  expect(keyboardOver({ ...s10, root: 491 })).toBe(0);
  expect(keyboardOver({ ...s10, root: 480 })).toBe(0);
});

test('no root yet: the height and the navigation bar under the keyboard', () => {
  expect(keyboardOver({ ...s10, root: 0 })).toBe(377.5);
  // iOS's height already covers the home indicator, and no root is read there.
  expect(keyboardOver({ ...s10, root: 820.5, android: false })).toBe(329.5);
});

test('a keyboard that is down lifts nothing', () => {
  expect(keyboardOver({ ...s10, height: 0, root: 820.5 })).toBe(0);
});
