// #55: contacting the project from inside the app (Settings > About >
// Contact us): a mail the person's own mail app sends, or a new issue on
// GitHub. The app sends nothing by itself. Every user event is awaited.

import { userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import RootLayout from '@/app/_layout';
import Settings from '@/app/settings';
import { reportAddress } from '@/ask/report';
import { setLang } from '@/i18n';
import { FakeAgent } from '@/test/fakeAgent';
import { contactMail, issuesUrl } from '@/ui/ContactSheet';

const routes = { _layout: RootLayout, settings: Settings };

beforeEach(() => {
  globalThis.fetch = new FakeAgent().fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function openSheet() {
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('button', { name: /^Contact us/ }));
  return { user, sheet: within(await screen.findByTestId('contact-sheet')) };
}

test('the mail the app writes goes to the project address, with the app version and room to write', () => {
  const mail = contactMail({ version: '0.2.0', os: 'Android' });
  expect(mail.to).toBe(reportAddress);
  expect(mail.subject).toBe('[Askorus] ');
  expect(mail.body).toBe('(Please write here)\n\n\nApp: Askorus 0.2.0 (Android)\n');
  expect(contactMail({ version: '', os: 'iOS' }).body).toContain('App: Askorus ? (iOS)');
});

test('Contact us opens the mail app with the mail written, and sends nothing itself', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const { user, sheet } = await openSheet();
  expect(sheet.getByText(/The app sends nothing by itself/)).toBeTruthy();
  expect(sheet.getByText(/No account is needed, and only we read it/)).toBeTruthy();
  expect(sheet.getByText(/It is public and needs a GitHub account/)).toBeTruthy();
  expect(open).not.toHaveBeenCalled();

  await user.press(sheet.getByRole('button', { name: 'Write a mail' }));
  expect(open).toHaveBeenCalledTimes(1);
  const link = String(open.mock.calls[0][0]);
  expect(link.startsWith(`mailto:${reportAddress}?subject=`)).toBe(true);
  expect(decodeURIComponent(link)).toContain('App: Askorus ');
  // The sheet closes: the rest happens in the mail app.
  await waitFor(() => expect(screen.queryByTestId('contact-sheet')).toBeNull());
});

test('without a mail app the address is shown and can be copied', async () => {
  jest.spyOn(Linking, 'openURL').mockRejectedValue(new Error('no app'));
  const { user, sheet } = await openSheet();
  await user.press(sheet.getByRole('button', { name: 'Write a mail' }));
  expect(await sheet.findByText(`No mail app could be opened. Write to ${reportAddress}.`)).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Copy the address' }));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(reportAddress);
  expect(await sheet.findByRole('button', { name: 'Copied.' })).toBeTruthy();
});

test('Write on GitHub opens the new issue forms', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const { user, sheet } = await openSheet();
  await user.press(sheet.getByRole('button', { name: 'Write on GitHub' }));
  expect(open).toHaveBeenCalledWith(issuesUrl);
  expect(issuesUrl).toBe('https://github.com/heeo-dev/askorus/issues/new/choose');
});

test('in Korean the row and the sheet read in Korean', async () => {
  setLang('ko');
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('button', { name: /^문의하기/ }));
  const sheet = within(await screen.findByTestId('contact-sheet'));
  expect(sheet.getByRole('button', { name: '메일 쓰기' })).toBeTruthy();
  expect(sheet.getByRole('button', { name: 'GitHub에 쓰기' })).toBeTruthy();
  expect(contactMail({ version: '0.2.0', os: 'Android' }).body).toContain('(여기에 적어 주세요)');
});
