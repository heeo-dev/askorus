// The question box and the follow-up bar at their sizes (S5c review): on a
// phone narrower than about 400 dp (or with a larger system font) the
// composer's toolbar packs closer, so the send button stays inside its box
// (UX-3); the follow-up grows with what is written (UX-7).

import { render, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { Composer, composerCompact, FollowBar } from './Composer';

const mockWindow = { width: 411, fontScale: 1 };
jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindow.width, height: 900, scale: 2.625, fontScale: mockWindow.fontScale }),
}));

beforeEach(() => {
  mockWindow.width = 411;
  mockWindow.fontScale = 1;
});

const pickLabel = 'AIs to answer: 3 (Claude, Codex, OpenCode). Pick';
const composer = () => (
  <Composer
    value=""
    onChangeText={() => undefined}
    onSend={() => undefined}
    canSend={false}
    placeholder="Ask a question"
    agents={['claude', 'codex', 'opencode']}
    pickLabel={pickLabel}
    onPickAIs={() => undefined}
    tier="standard"
    onTier={() => undefined}
  />
);
const style = (el: { props: { style?: unknown } }) => StyleSheet.flatten(el.props.style as never) ?? {};
const pill = (name: string) => style(screen.getByRole('radio', { name }));

test('below about 400 dp, or with a larger font, the toolbar packs closer', () => {
  expect(composerCompact(411, 1)).toBe(false);
  expect(composerCompact(400, 1)).toBe(false);
  expect(composerCompact(399, 1)).toBe(true);
  expect(composerCompact(360, 1)).toBe(true);
  expect(composerCompact(411, 1.3)).toBe(true); // 316 dp of text room
  expect(composerCompact(480, 1.15)).toBe(false);
});

test('a wide phone keeps the count and the board’s padding', async () => {
  await render(composer());
  expect(screen.getByText('3')).toBeTruthy();
  expect(style(screen.getByRole('button', { name: pickLabel })).paddingHorizontal).toBe(8);
  expect(['Quick', 'Standard', 'Deep'].map((x) => pill(x).paddingHorizontal)).toEqual([10, 10, 10]);
});

test('on a 360 dp phone the count goes (the label still says it), the padding is cut and the tier may shrink, never the send button (UX-3)', async () => {
  mockWindow.width = 360;
  await render(composer());
  expect(screen.queryByText('3')).toBeNull();
  const pick = screen.getByRole('button', { name: pickLabel });
  expect(style(pick).paddingHorizontal).toBe(4);
  expect(['Quick', 'Standard', 'Deep'].map((x) => pill(x).paddingHorizontal)).toEqual([6, 6, 6]);
  // Last resort: the tier row and its pills give way; the send button and the pile do not.
  expect(style(screen.getByLabelText('Tier'))).toMatchObject({ flexShrink: 1, minWidth: 0 });
  expect(pill('Deep')).toMatchObject({ flexShrink: 1, minWidth: 0 });
  expect(style(screen.getByRole('button', { name: 'Ask' })).flexShrink).toBe(0);
  expect(style(pick).flexShrink).toBe(0);
});

test('the follow-up box grows with what is written, from one line to about five (UX-7)', async () => {
  await render(<FollowBar value="" onChangeText={() => undefined} onSend={() => undefined} canSend placeholder="Follow up" sendLabel="Send" />);
  const box = screen.getByLabelText('Follow up');
  expect(box.props.multiline).toBe(true);
  expect(style(box)).toMatchObject({ minHeight: 44, maxHeight: 120, paddingTop: 11, paddingBottom: 11, borderRadius: 22 });
  expect(style(box).height).toBeUndefined();
});
