// Room for the on-screen keyboard and the system bars (S5c). Android draws
// the app edge to edge and does not resize it for the keyboard, and
// KeyboardAvoidingView did nothing on the S23 (see FormScroll), so a bar at
// the bottom of a screen lifts itself: its bottom padding is
// max(useKeyboardInset(), useSafeInsets().bottom).

import { useContext, useEffect, useState } from 'react';
import { Keyboard, Platform } from 'react-native';
import { type EdgeInsets, SafeAreaInsetsContext } from 'react-native-safe-area-context';

const none: EdgeInsets = { top: 0, right: 0, bottom: 0, left: 0 };

/** useSafeInsets is the system bars' room (status bar, navigation bar); zero outside a SafeAreaProvider. */
export function useSafeInsets(): EdgeInsets {
  return useContext(SafeAreaInsetsContext) ?? none;
}

/**
 * useKeyboardInset is how far the keyboard reaches up from the bottom of
 * the screen, 0 while it is down. On Android the keyboard event's height
 * leaves out the navigation bar under the keyboard, so it is added back;
 * on iOS the height already covers the home indicator.
 */
export function useKeyboardInset(): number {
  const bottom = useSafeInsets().bottom;
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const ios = Platform.OS === 'ios';
    const shown = Keyboard.addListener(ios ? 'keyboardWillShow' : 'keyboardDidShow', (e) => setHeight(e.endCoordinates.height));
    const hidden = Keyboard.addListener(ios ? 'keyboardWillHide' : 'keyboardDidHide', () => setHeight(0));
    return () => {
      shown.remove();
      hidden.remove();
    };
  }, []);
  if (height <= 0) return 0;
  return Platform.OS === 'android' ? height + bottom : height;
}
