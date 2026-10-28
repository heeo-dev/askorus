// A scrolling screen with text inputs. When the on-screen keyboard opens,
// the screen shrinks to the space above it and the focused input moves into
// view with room below it for the button that usually follows (the keyboard
// used to cover both). React Native only, so it also runs in Expo Go, where
// react-native-keyboard-controller is not available.

import { type ReactNode, useEffect, useRef } from 'react';
import {
  Keyboard,
  KeyboardAvoidingView,
  ScrollView,
  type ScrollViewInstance,
  type StyleProp,
  StyleSheet,
  TextInput,
  type ViewStyle,
} from 'react-native';

// Room kept visible under the focused input: about one button and a gap.
const roomBelow = 96;

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

  useEffect(() => {
    const sub = Keyboard.addListener('keyboardDidShow', (e) => {
      const keyboardTop = e.endCoordinates.screenY;
      // Wait one moment so the shrunken screen has been laid out.
      setTimeout(() => {
        const input = TextInput.State.currentlyFocusedInput();
        if (!input || !scroll.current) return;
        input.measureInWindow((_x, y, _w, h) => {
          const hidden = y + h + roomBelow - keyboardTop;
          if (hidden > 0) scroll.current?.scrollTo({ y: offset.current + hidden, animated: true });
        });
      }, 80);
    });
    return () => sub.remove();
  }, []);

  return (
    <KeyboardAvoidingView style={s.fill} behavior="padding">
      <ScrollView
        ref={scroll}
        style={style}
        contentContainerStyle={contentContainerStyle}
        keyboardShouldPersistTaps="handled"
        scrollEventThrottle={32}
        onScroll={(e) => {
          offset.current = e.nativeEvent.contentOffset.y;
        }}
      >
        {children}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({ fill: { flex: 1 } });
