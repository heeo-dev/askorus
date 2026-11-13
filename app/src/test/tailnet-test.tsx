// Settings > Tailscale (#57): Tailscale is inside the app, and this row with
// its sheet is all there is to set of it: who is signed in, under which
// name, signing in and out.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Alert, Linking } from 'react-native';

import RootLayout from '@/app/_layout';
import Settings from '@/app/settings';
import { FakeAgent } from '@/test/fakeAgent';
import { LOGIN_URL, mockTailnet, neverSignedIn } from '@/test/tailnet';

const routes = { _layout: RootLayout, settings: Settings };

beforeEach(() => {
  globalThis.fetch = new FakeAgent().fetch;
});

afterEach(() => jest.restoreAllMocks());

test('the row names the account; the sheet shows the phone and signs out after asking', async () => {
  const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/settings' });
  const row = await screen.findByRole('button', { name: /^Tailscale/ });
  await waitFor(() => expect(row).toHaveAccessibilityValue({ text: 'me@example.com' }));

  await user.press(row);
  const sheet = within(await screen.findByTestId('tailnet-sheet'));
  expect(await sheet.findByText('Connected to Tailscale')).toBeTruthy();
  expect(sheet.getByText('askorus-test-phone.tail0.ts.net')).toBeTruthy();
  expect(sheet.getByText(/does not use the phone's VPN and does not affect other apps/)).toBeTruthy();

  await user.press(sheet.getByRole('button', { name: 'Sign out of Tailscale' }));
  expect(alert).toHaveBeenCalledWith('Sign out of Tailscale?', expect.stringContaining('Paired PCs and your conversations stay'), expect.any(Array));
  expect(mockTailnet.calls).not.toContain('logout'); // not before the person says so
  const buttons = alert.mock.calls[0][2] as Array<{ text: string; onPress?: () => void }>;
  buttons.find((b) => b.text === 'Sign out of Tailscale')?.onPress?.();
  await waitFor(() => expect(mockTailnet.calls).toEqual(expect.arrayContaining(['logout', 'stop'])));
  await waitFor(() => expect(screen.getByRole('button', { name: /^Tailscale/ })).toHaveAccessibilityValue({ text: 'Not signed in' }));
});

test('a phone that never signed in: the row says so and the sheet signs in', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await neverSignedIn();
  await renderRouter(routes, { initialUrl: '/settings' });
  const row = await screen.findByRole('button', { name: /^Tailscale/ });
  await waitFor(() => expect(row).toHaveAccessibilityValue({ text: 'Not signed in' }));
  expect(mockTailnet.calls).toEqual([]);

  await user.press(row);
  const sheet = within(await screen.findByTestId('tailnet-sheet'));
  expect(sheet.queryByRole('button', { name: 'Sign out of Tailscale' })).toBeNull();
  await user.press(await sheet.findByRole('button', { name: 'Sign in to Tailscale' }));
  await waitFor(() => expect(open).toHaveBeenCalledWith(LOGIN_URL));
});
