// A scrolling screen with text inputs. While the on-screen keyboard is up,
// the content gets room at its end as tall as the keyboard, and the focused
// input moves into view with room below it for the button that usually
// follows (the keyboard used to cover both). The room is added here rather
// than left to KeyboardAvoidingView, which did nothing on a Galaxy S23
// (10-28), so an input at the very end of a screen could not be scrolled
// up. The input is brought into view again whenever the content changes
// size, such as when that room appears or a button shows up under the
// input. React Native only, so it also runs in Expo Go, where
// react-native-keyboard-controller is not available.

import { type ReactNode, useEffect, useRef, useState } from 'react';
import { Keyboard, ScrollView, type ScrollViewInstance, type StyleProp, TextInput, View, type ViewStyle } from 'react-native';

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
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [keyboard, setKeyboard] = useState(0);

  // Only while the keyboard is up, and never after the screen is gone.
  const later = (ms: number) => {
    if (keyboardTop.current === null) return;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(showFocused, ms);
  };

  const showFocused = () => {
    const top = keyboardTop.current;
    const input = TextInput.State.currentlyFocusedInput();
    if (top === null || !input || !scroll.current) return;
    input.measureInWindow((_x, y, _w, h) => {
      const hidden = y + h + roomBelow - top;
      if (hidden > 1) scroll.current?.scrollTo({ y: offset.current + hidden, animated: true });
    });
  };

  useEffect(() => {
    const shown = Keyboard.addListener('keyboardDidShow', (e) => {
      keyboardTop.current = e.endCoordinates.screenY;
      setKeyboard(e.endCoordinates.height);
      later(50); // also when the room was already there
    });
    const hidden = Keyboard.addListener('keyboardDidHide', () => {
      keyboardTop.current = null;
      setKeyboard(0);
    });
    return () => {
      shown.remove();
      hidden.remove();
      if (timer.current) clearTimeout(timer.current);
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
    >
      {children}
      {keyboard > 0 ? <View style={{ height: keyboard }} testID="keyboard-room" /> : null}
    </ScrollView>
  );
}
