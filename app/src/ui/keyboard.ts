// Room for the on-screen keyboard and the system bars (S5c). A bar at the
// bottom of a screen lifts itself: its bottom padding is
// max(useKeyboardInset(), useSafeInsets().bottom). KeyboardAvoidingView did
// nothing on the S23 (see FormScroll). Android may or may not shrink the
// app for the keyboard: the S23 (edge to edge) does not, the S10 (Android
// 12, Expo Go) does, and lifting a bar by the keyboard's height in a window
// that already shrank pushed the new-session form out of sight (R-62). So
// the lift is how far the keyboard reaches over the app's root view, whose
// bottom the root layout reports (KeyboardRoot).

import { useContext, useEffect, useState, useSyncExternalStore } from 'react';
import { Keyboard, Platform } from 'react-native';
import { type EdgeInsets, SafeAreaInsetsContext } from 'react-native-safe-area-context';

const none: EdgeInsets = { top: 0, right: 0, bottom: 0, left: 0 };

/** useSafeInsets is the system bars' room (status bar, navigation bar); zero outside a SafeAreaProvider. */
export function useSafeInsets(): EdgeInsets {
  return useContext(SafeAreaInsetsContext) ?? none;
}

// The root view's bottom on the screen (0: not laid out yet), from the
// root layout's onLayout.
let rootBottom = 0;
const rootListeners = new Set<() => void>();

/** setRootBottom records where the app's root view ends on the screen. */
export function setRootBottom(y: number) {
  if (y === rootBottom) return;
  rootBottom = y;
  for (const l of rootListeners) l();
}

function useRootBottom(): number {
  return useSyncExternalStore(
    (l) => {
      rootListeners.add(l);
      return () => rootListeners.delete(l);
    },
    () => rootBottom,
  );
}

/**
 * keyboardOver is how far a keyboard whose top is at keyboardTop (screen
 * y) reaches over a root view ending at root: 0 when the window shrank to
 * end above it. Without a root (not laid out, tests) it is the keyboard's
 * height, plus on Android the navigation bar under the keyboard, which the
 * event's height leaves out (iOS's covers the home indicator).
 */
export function keyboardOver(o: { keyboardTop: number; height: number; root: number; navBar: number; android: boolean }): number {
  if (o.height <= 0) return 0;
  if (o.android && o.root > 0) return Math.max(0, Math.round(o.root - o.keyboardTop));
  return o.android ? o.height + o.navBar : o.height;
}

/** useKeyboardInset is how far the keyboard reaches over the app from below, 0 while it is down. */
export function useKeyboardInset(): number {
  const bottom = useSafeInsets().bottom;
  const root = useRootBottom();
  const [kb, setKb] = useState({ top: 0, height: 0 });
  useEffect(() => {
    const ios = Platform.OS === 'ios';
    const shown = Keyboard.addListener(ios ? 'keyboardWillShow' : 'keyboardDidShow', (e) =>
      setKb({ top: e.endCoordinates.screenY, height: e.endCoordinates.height }),
    );
    const hidden = Keyboard.addListener(ios ? 'keyboardWillHide' : 'keyboardDidHide', () => setKb({ top: 0, height: 0 }));
    return () => {
      shown.remove();
      hidden.remove();
    };
  }, []);
  return keyboardOver({ keyboardTop: kb.top, height: kb.height, root, navBar: bottom, android: Platform.OS === 'android' });
}

/** rootBottomNow is the root view's bottom right now, for code outside React's render (FormScroll). */
export function rootBottomNow(): number {
  return rootBottom;
}
