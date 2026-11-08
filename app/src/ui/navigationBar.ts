// The Android navigation bar follows the app's theme (#17, R-82). React
// Native sets the bar from the phone's light or dark once, as the app starts
// (its enableEdgeToEdge in ReactActivityDelegate.onCreate), and every Modal
// window from the phone's again; the activity keeps uiMode changes to
// itself, so nothing sets the bar later. A theme chosen in the app, or the
// phone's changed while the app ran, left the bar the other way (a white
// bar under a dark screen on the S10).
//
// The app's own native module (modules/system-bars) sets the bar on the app
// window and on the open Modal windows, and gives a new Modal window the
// app's bar. expo-navigation-bar could not: on Android 12 its androidx call
// changes an old flag that Android ignores once React Native's StatusBar has
// set the bars the new way (see SystemBarsModule.kt). It is called on every
// change, with no memory of the last call, because the splash screen puts
// the bar back the way it was at start-up when it goes (the root layout
// calls it again once the launch cover is done).

import { requireOptionalNativeModule } from 'expo';
import { Platform } from 'react-native';

type SystemBarsModule = { setNavigationBarLight?: (light: boolean) => Promise<void> };

let native: SystemBarsModule | null | undefined;

/** Sets the navigation bar for the app's scheme (Android only): a light bar with dark buttons for the light theme. */
export function applyNavigationBar(scheme: 'light' | 'dark'): void {
  if (Platform.OS !== 'android') return;
  if (native === undefined) native = requireOptionalNativeModule<SystemBarsModule>('AskorusSystemBars');
  if (!native || typeof native.setNavigationBarLight !== 'function') return;
  native.setNavigationBarLight(scheme === 'light').catch((e: unknown) => console.warn('[navigation bar]', e));
}
