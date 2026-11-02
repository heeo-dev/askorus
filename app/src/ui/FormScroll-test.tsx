// R-16: while the keyboard is up the form gets room at its end, so an
// input that is the last thing on a screen (the pair link) can still be
// scrolled above the keyboard. KeyboardAvoidingView did nothing on a
// Galaxy S23, so the room cannot be left to it.

import { act, render, screen } from '@testing-library/react-native';
import { Keyboard, Platform, Text, TextInput } from 'react-native';

import { FormScroll } from './FormScroll';
import { setRootBottom } from './keyboard';

test('the form gets room as tall as the keyboard while it is up', async () => {
  const listeners: Record<string, (e: any) => void> = {};
  jest.spyOn(Keyboard, 'addListener').mockImplementation(((name: string, cb: (e: any) => void) => {
    listeners[name] = cb;
    return { remove: () => undefined } as any;
  }) as any);

  await render(
    <FormScroll>
      <Text>Paste the link</Text>
      <TextInput placeholder="asksemble://pair?…" />
    </FormScroll>,
  );
  expect(screen.queryByTestId('keyboard-room')).toBeNull();

  await act(async () => listeners.keyboardDidShow({ endCoordinates: { height: 320, screenY: 500 } }));
  expect(screen.getByTestId('keyboard-room').props.style).toEqual({ height: 320 });

  await act(async () => listeners.keyboardDidHide({}));
  expect(screen.queryByTestId('keyboard-room')).toBeNull();
  jest.restoreAllMocks();
});

// R-62: on Android the room is what the keyboard covers of the app. A
// window that shrank to end at the keyboard (S10, Expo Go) needs none.
test('on Android, no room when the window already shrank for the keyboard', async () => {
  const listeners: Record<string, (e: any) => void> = {};
  jest.spyOn(Keyboard, 'addListener').mockImplementation(((name: string, cb: (e: any) => void) => {
    listeners[name] = cb;
    return { remove: () => undefined } as any;
  }) as any);
  jest.replaceProperty(Platform, 'OS', 'android');
  try {
    await render(
      <FormScroll>
        <TextInput placeholder="Name" />
      </FormScroll>,
    );
    setRootBottom(500);
    await act(async () => listeners.keyboardDidShow({ endCoordinates: { height: 320, screenY: 500 } }));
    expect(screen.queryByTestId('keyboard-room')).toBeNull();

    await act(async () => listeners.keyboardDidHide({}));
    setRootBottom(820);
    await act(async () => listeners.keyboardDidShow({ endCoordinates: { height: 320, screenY: 500 } }));
    expect(screen.getByTestId('keyboard-room').props.style).toEqual({ height: 320 });
  } finally {
    setRootBottom(0);
    jest.restoreAllMocks();
  }
});
