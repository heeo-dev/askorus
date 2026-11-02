// R-58 (#2): a sheet's Modal slid up as a whole, so the dimmed screen came
// up from the bottom with the sheet. The Modal fades now and only the
// sheet moves; the scrim is outside the moving view.

import { render, screen } from '@testing-library/react-native';
import { StatusBar, Text } from 'react-native';

import { Sheet } from './Sheet';

const mockScheme = { value: 'light' as 'light' | 'dark' | null };
jest.mock('react-native/Libraries/Utilities/useColorScheme', () => ({ __esModule: true, default: () => mockScheme.value }));

test('the Modal fades and only the sheet slides', async () => {
  await render(
    <Sheet open onClose={() => undefined} title="AIs to answer" testID="s">
      <Text>body</Text>
    </Sheet>,
  );
  const sheet = screen.getByTestId('s');
  let modal = sheet.parent;
  while (modal && modal.props.animationType === undefined) modal = modal.parent;
  expect(modal?.props.animationType).toBe('fade');
  const moves = (style: unknown) => JSON.stringify(style ?? null).includes('translateY');
  expect(moves(sheet.props.style)).toBe(true);
  // The scrim is not inside the sliding sheet.
  let node = screen.getByTestId('s-scrim', { includeHiddenElements: true }).parent;
  while (node) {
    expect(node.props.testID).not.toBe('s');
    node = node.parent;
  }
});

// R-64: on the S10 (Android 12) a Modal's window did not get the app's dark
// status bar icons, so the light drawer showed white ones. A Modal sets the
// theme's look again once it shows (StatusBar.setBarStyle reaches Modal
// windows too).
test.each([
  ['light', 'dark-content'],
  ['dark', 'light-content'],
] as const)('an open sheet sets the %s theme’s status bar look', async (scheme, look) => {
  mockScheme.value = scheme;
  const set = jest.spyOn(StatusBar, 'setBarStyle').mockImplementation(() => undefined);
  try {
    await render(
      <Sheet open onClose={() => undefined} testID="s">
        <Text>body</Text>
      </Sheet>,
    );
    let modal = screen.getByTestId('s').parent;
    while (modal && modal.props.onShow === undefined) modal = modal.parent;
    modal?.props.onShow();
    expect(set).toHaveBeenCalledWith(look);
  } finally {
    set.mockRestore();
    mockScheme.value = 'light';
  }
});

test('a closed sheet renders nothing, and opening it again slides again', async () => {
  const view = await render(
    <Sheet open={false} onClose={() => undefined} testID="s">
      <Text>body</Text>
    </Sheet>,
  );
  expect(screen.queryByTestId('s')).toBeNull();
  await view.rerender(
    <Sheet open onClose={() => undefined} testID="s">
      <Text>body</Text>
    </Sheet>,
  );
  expect(screen.getByTestId('s')).toBeTruthy();
});
