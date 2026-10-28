// R-16: while the keyboard is up the form gets room at its end, so an
// input that is the last thing on a screen (the pair link) can still be
// scrolled above the keyboard. KeyboardAvoidingView did nothing on a
// Galaxy S23, so the room cannot be left to it.

import { act, render, screen } from '@testing-library/react-native';
import { Keyboard, Text, TextInput } from 'react-native';

import { FormScroll } from './FormScroll';

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
