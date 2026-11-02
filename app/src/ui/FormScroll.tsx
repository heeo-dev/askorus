// A scrolling screen with text inputs. While the on-screen keyboard is up,
// the content gets room at its end as tall as the keyboard, and the focused
// input moves into view with room below it for the button that usually
// follows (the keyboard used to cover both). The room is added here rather
// than left to KeyboardAvoidingView, which did nothing on a Galaxy S23
// (10-28), so an input at the very end of a screen could not be scrolled
// up. The input is brought into view again whenever the content or the
// view changes size, such as when that room appears, a button shows up
// under the input or the window shrinks for the keyboard. React Native only, so it also runs in Expo Go, where
// react-native-keyboard-controller is not available.

import { type ReactNode, useEffect, useRef, useState } from 'react';
import { Keyboard, Platform, ScrollView, type ScrollViewInstance, type StyleProp, TextInput, View, type ViewStyle } from 'react-native';

import { keyboardOver, rootBottomNow } from './keyboard';

// Room kept visible under the focused input: about one button and a gap.
const roomBelow = 120;

export function FormScroll({
  style,
  contentContainerStyle,
  children,
}: {
  style?: StyleProp<ViewStyle>;
  contentContainerStyle?: StyleProp<ViewStyle>;
  children: ReactNode;
}) {
  const scroll = useRef<ScrollViewInstance>(null);
  const offset = useRef(0);
  const keyboardTop = useRef<number | null>(null);
  // The input that had the focus when the keyboard came up: while the
  // window shrinks for the keyboard (S10, R-62) the focus can be away for a
  // moment, and the input still has to come into view.
  const focused = useRef<ReturnType<typeof TextInput.State.currentlyFocusedInput>>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [keyboard, setKeyboard] = useState(0);

  // Only while the keyboard is up, and never after the screen is gone.
  const later = (ms: number) => {
    if (keyboardTop.current === null) return;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(showFocused, ms);
  };
  const settled = useRef<ReturnType<typeof setTimeout> | null>(null);
  const later2 = (ms: number) => {
    if (settled.current) clearTimeout(settled.current);
    settled.current = setTimeout(showFocused, ms);
  };

  const showFocused = () => {
    const top = keyboardTop.current;
    const input = TextInput.State.currentlyFocusedInput() ?? focused.current;
    const view = scroll.current;
    if (top === null || !input || !view) return;
    // What can be seen ends where the view ends (a bar under it) or where
    // the keyboard starts, whichever is higher. The view and the input are
    // measured alike: in Expo Go on the S10 the window's coordinates sat
    // about 44 points off the keyboard event's screen ones (R-62).
    view.measureInWindow((_vx, vy, _vw, vh) => {
      input.measureInWindow((_x, y, _w, h) => {
        const hidden = y + h + roomBelow - Math.min(vy + vh, top);
        if (hidden > 1) view.scrollTo({ y: offset.current + hidden, animated: true });
      });
    });
  };

  useEffect(() => {
    const shown = Keyboard.addListener('keyboardDidShow', (e) => {
      keyboardTop.current = e.endCoordinates.screenY;
      focused.current = TextInput.State.currentlyFocusedInput();
      // As much room as the keyboard covers of the app (R-62: none when
      // the window shrank for it).
      const over = keyboardOver({ keyboardTop: e.endCoordinates.screenY, height: e.endCoordinates.height, root: rootBottomNow(), navBar: 0, android: Platform.OS === 'android' });
      setKeyboard(over);
      later(50); // also when the room was already there
      later2(400); // and once the window and the bars have settled
    });
    const hidden = Keyboard.addListener('keyboardDidHide', () => {
      keyboardTop.current = null;
      focused.current = null;
      setKeyboard(0);
    });
    return () => {
      shown.remove();
      hidden.remove();
      if (timer.current) clearTimeout(timer.current);
      if (settled.current) clearTimeout(settled.current);
    };
  }, []);

  return (
    <ScrollView
      ref={scroll}
      style={style}
      contentContainerStyle={contentContainerStyle}
      keyboardShouldPersistTaps="handled"
      scrollEventThrottle={32}
      onScroll={(e) => {
        offset.current = e.nativeEvent.contentOffset.y;
      }}
      onContentSizeChange={() => later(0)}
      // The view itself shrinks when the window does for the keyboard or a
      // bar under it grows (R-62): bring the input into view again then.
      onLayout={() => later(0)}
    >
      {children}
      {keyboard > 0 ? <View style={{ height: keyboard }} testID="keyboard-room" /> : null}
    </ScrollView>
  );
}
