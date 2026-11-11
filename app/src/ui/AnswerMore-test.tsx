// Share and report behind an answer's '...' (#50): Google Play asks for a
// way to report an AI's answer from inside the app, and Askorus has no
// server, so the report goes out through the person's own mail app.
import { render, screen, userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { Linking, Share } from 'react-native';

import type { PartView } from '@/ask/present';
import { reportAddress } from '@/ask/report';

import { AnswerActions, AnswerBlock } from './AnswerBlock';
import { SheetEnd } from './Sheet';

const view: PartView = {
  agent: 'claude',
  name: 'Claude',
  model: 'sonnet',
  state: 'done',
  s: 3.1,
  part: { answer: 'Paris is the capital.', ms: 3100 } as unknown as PartView['part'],
};
const more = { ai: 'Claude sonnet', text: 'Paris is the capital.', question: 'What is the capital of France?' };

const mailOf = (link: string) => {
  const query = link.slice(link.indexOf('?') + 1);
  return Object.fromEntries(query.split('&').map((kv) => kv.split('=').map(decodeURIComponent) as [string, string]));
};

afterEach(() => {
  jest.restoreAllMocks();
  jest.clearAllMocks();
});

test("an answer's actions end with '...' only when there is something to share or report", async () => {
  const { rerender } = await render(<AnswerBlock p={view} onCopy={() => undefined} />);
  expect(screen.getByRole('button', { name: 'Copy' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Share or report' })).toBeNull();

  await rerender(<AnswerBlock p={view} onCopy={() => undefined} more={more} />);
  expect(screen.getByRole('button', { name: 'Share or report' })).toBeTruthy();
  // Not while the answer is still coming.
  await rerender(<AnswerBlock p={{ ...view, state: 'answering' }} onCopy={() => undefined} more={more} />);
  expect(screen.queryByRole('button', { name: 'Share or report' })).toBeNull();
});

test('share hands the answer text to the share window, with nothing added', async () => {
  const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction', activityType: undefined });
  const user = userEvent.setup();
  await render(<AnswerActions onCopy={() => undefined} more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  const sheet = within(screen.getByTestId('answer-more'));
  expect(sheet.getByRole('button', { name: 'Report' })).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Share' }));
  expect(share).toHaveBeenCalledWith({ message: 'Paris is the capital.' });
  expect(screen.queryByTestId('answer-more')).toBeNull();
});

test('a share the person backs out of, or that fails, says nothing', async () => {
  jest.spyOn(Share, 'share').mockRejectedValue(new Error('no activity'));
  const user = userEvent.setup();
  await render(<AnswerActions onCopy={() => undefined} more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Share' }));
  expect(screen.queryByTestId('answer-more')).toBeNull();
  expect(screen.queryByText(/mail app/)).toBeNull();
});

test('report: pick what is wrong, and the mail app opens with the report; the answer text stays out by default', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<AnswerActions onCopy={() => undefined} more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));

  const sheet = within(screen.getByTestId('answer-report'));
  expect(sheet.getByRole('header', { name: 'Report an AI answer' })).toBeTruthy();
  expect(sheet.getByText('Claude sonnet')).toBeTruthy();
  expect(sheet.getByText('The app sends nothing by itself. Your mail app opens with the report written, and you can change it before you send it.')).toBeTruthy();
  expect(sheet.getAllByRole('radio')).toHaveLength(5);
  // Nothing to send until a kind is picked.
  expect(sheet.getByRole('button', { name: 'Open mail app' })).toBeDisabled();
  expect(sheet.getByRole('switch', { name: 'Include the question and the answer' })).not.toBeChecked();
  expect(sheet.getByText('Without them the mail says only what kind of problem it is and which AI wrote the answer.')).toBeTruthy();

  await user.press(sheet.getByRole('radio', { name: 'Violence or dangerous acts' }));
  expect(sheet.getByRole('radio', { name: 'Violence or dangerous acts' })).toBeChecked();
  await user.press(sheet.getByRole('button', { name: 'Open mail app' }));

  expect(open).toHaveBeenCalledTimes(1);
  const link = open.mock.calls[0][0];
  expect(link.startsWith(`mailto:${reportAddress}?`)).toBe(true);
  const mail = mailOf(link);
  expect(mail.subject).toBe('[Askorus report] Violence or dangerous acts');
  expect(mail.body).toContain('Written by: Claude sonnet');
  expect(mail.body).not.toContain('Paris');
  expect(mail.body).not.toContain('capital of France');

  // The sheet goes, and the answer says the report is not sent yet.
  expect(screen.queryByTestId('answer-report')).toBeNull();
  expect(screen.getByText('The report is sent when you press Send in your mail app.')).toBeTruthy();
});

test('report with the question and the answer: the sheet shows what will go, and it goes', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<AnswerActions more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  const sheet = within(screen.getByTestId('answer-report'));
  expect(sheet.queryByTestId('report-quoted')).toBeNull();

  await user.press(sheet.getByRole('switch', { name: 'Include the question and the answer' }));
  expect(sheet.getByText('This goes into the mail:')).toBeTruthy();
  const quoted = sheet.getByTestId('report-quoted');
  expect(quoted).toHaveTextContent(/What is the capital of France\?/);
  expect(quoted).toHaveTextContent(/Paris is the capital\./);

  await user.press(sheet.getByRole('radio', { name: 'Something else' }));
  await user.press(sheet.getByRole('button', { name: 'Open mail app' }));
  const mail = mailOf(open.mock.calls[0][0]);
  expect(mail.body).toContain('--- Question ---\r\nWhat is the capital of France?');
  expect(mail.body).toContain('--- Answer ---\r\nParis is the capital.');
});

test('no mail app: the sheet stays, names the address and offers the report to copy', async () => {
  jest.spyOn(Linking, 'openURL').mockRejectedValue(new Error('No Activity found to handle Intent'));
  const user = userEvent.setup();
  await render(<AnswerActions more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  const sheet = within(screen.getByTestId('answer-report'));
  await user.press(sheet.getByRole('radio', { name: 'Hate or discrimination' }));
  expect(sheet.queryByRole('button', { name: 'Copy the report' })).toBeNull();
  await user.press(sheet.getByRole('button', { name: 'Open mail app' }));

  expect(sheet.getByText(`No mail app could be opened. Copy the report and send it to ${reportAddress}.`)).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Copy the report' }));
  const copied = (Clipboard.setStringAsync as jest.Mock).mock.calls.at(-1)?.[0] as string;
  expect(copied.startsWith(`${reportAddress}\n[Askorus report] Hate or discrimination\n\nProblem: Hate or discrimination`)).toBe(true);
  expect(sheet.getByRole('button', { name: 'Copied.' })).toBeTruthy();
  // Nothing claims the report is on its way.
  expect(screen.queryByText('The report is sent when you press Send in your mail app.')).toBeNull();
});

test('each opening starts at the menu again, with nothing picked', async () => {
  jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<AnswerActions more={more} />);
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('radio', { name: 'Self-harm' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('button', { name: 'Close' }));
  expect(screen.queryByTestId('answer-report')).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  expect(screen.getByTestId('answer-more')).toBeTruthy();
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  expect(within(screen.getByTestId('answer-report')).getByRole('radio', { name: 'Self-harm' })).not.toBeChecked();
});

test("what the screen says after the report gives way to the screen's next line (a copy)", async () => {
  jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  const { rerender } = await render(<AnswerActions more={more} note={{ text: 'Copied.' }} />);
  expect(screen.getByText('Copied.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('radio', { name: 'Sexual content' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('button', { name: 'Open mail app' }));
  expect(screen.getByText('The report is sent when you press Send in your mail app.')).toBeTruthy();
  expect(screen.queryByText('Copied.')).toBeNull();

  await rerender(<AnswerActions more={more} note={{ text: 'Could not open the Claude app.', error: true }} />);
  expect(screen.getByText('Could not open the Claude app.')).toBeTruthy();
  expect(screen.queryByText('The report is sent when you press Send in your mail app.')).toBeNull();
});

test('inside a sheet, a line that appears under the actions is brought into view; one already there is not', async () => {
  jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const toEnd = jest.fn();
  const user = userEvent.setup();
  const { rerender } = await render(
    <SheetEnd.Provider value={toEnd}>
      <AnswerActions more={more} note={{ text: 'Copied.' }} />
    </SheetEnd.Provider>,
  );
  // The sheet opened with that line: it stays where the person is reading.
  expect(toEnd).not.toHaveBeenCalled();

  await user.press(screen.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('radio', { name: 'Self-harm' }));
  await user.press(within(screen.getByTestId('answer-report')).getByRole('button', { name: 'Open mail app' }));
  expect(screen.getByText('The report is sent when you press Send in your mail app.')).toBeTruthy();
  expect(toEnd).toHaveBeenCalledTimes(1);

  // The same for the screen's own lines (a copy made in the answer sheet).
  await rerender(
    <SheetEnd.Provider value={toEnd}>
      <AnswerActions more={more} note={{ text: 'Could not open the Claude app.', error: true }} />
    </SheetEnd.Provider>,
  );
  expect(toEnd).toHaveBeenCalledTimes(2);
  // When the screen's line goes, the report's line is back: a line again.
  await rerender(
    <SheetEnd.Provider value={toEnd}>
      <AnswerActions more={more} />
    </SheetEnd.Provider>,
  );
  expect(screen.getByText('The report is sent when you press Send in your mail app.')).toBeTruthy();
  expect(toEnd).toHaveBeenCalledTimes(3);
});
