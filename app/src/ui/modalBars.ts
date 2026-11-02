// A Modal is a window of its own on Android, with its own status bar look.
// React Native copies the app's look to it as it opens, but on the S10
// (Android 12) the copy read "light bars: no", so the drawer's light panel
// got white status bar icons (R-64). StatusBar.setBarStyle reaches every
// open Modal window as well, so a Modal sets the theme's look again once it
// shows (the root's StatusBar does not: its style has not changed).

import { StatusBar } from 'react-native';

import type { Scheme } from './theme';

/** modalBars sets the status bar look for the scheme on the app and its open Modals. */
export function modalBars(scheme: Scheme) {
  StatusBar.setBarStyle(scheme === 'dark' ? 'light-content' : 'dark-content');
}
