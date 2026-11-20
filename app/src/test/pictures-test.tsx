// The row of every AI's drawings under a combined answer (#14, decisions 6
// and 10 of 2026-11-21): when several AIs each drew an SVG, the combined
// answer showed one drawing and the others took a press on each AI's card.
// The row shows them side by side under their AI's names, in pages without
// script, and opens one large. Every user event is awaited (R-07).

import { render, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { mockPhotoSave } from '@/test/photos';
import { drawingsIn } from '@/ui/Markdown';
import { maxPerAI, maxTiles, PictureRow, rowPictures } from '@/ui/PictureRow';
import { svgTilePage } from '@/ui/svg';

const svg = (colour: string) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" width="10" height="10"><circle cx="5" cy="5" r="4" fill="${colour}"/></svg>`;
const drawn = (colour: string) => `<svg viewBox="0 0 10 10" width="10" height="10"><circle cx="5" cy="5" r="4" fill="${colour}"/></svg>`;
const block = (colour: string) => '```svg\n' + svg(colour) + '\n```';
const tick = '```';

const answers = [
  { agent: 'claude', name: 'Claude', text: `Here is a cat.\n\n${block('red')}\n\nIt is red.` },
  { agent: 'codex', name: 'Codex', text: `${block('green')}\n\n${block('blue')}` },
  { agent: 'opencode', name: 'OpenCode', text: `I cannot draw, but:\n\n${tick}\n /\\_/\\\n( o.o )\n${tick}` },
];

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

test('the drawings of an answer are its SVG code blocks, written again as drawings only', () => {
  expect(drawingsIn(answers[0].text)).toEqual([{ svg: drawn('red'), source: svg('red') }]);
  expect(drawingsIn(answers[1].text).map((d) => d.svg)).toEqual([drawn('green'), drawn('blue')]);
  // Text art, other code and an SVG that is more than a drawing are none.
  expect(drawingsIn(answers[2].text)).toEqual([]);
  expect(drawingsIn('```js\nconst a = "<svg>";\n```')).toEqual([]);
  expect(drawingsIn('```svg\n<svg><script>alert(1)</script></svg>\n```')).toEqual([]);
  expect(drawingsIn('An <svg> tag in a sentence is not a block.')).toEqual([]);
  // Inside a list or a quote too, and no more than an answer shows as pictures (six, R-123).
  expect(drawingsIn(`- one\n\n  ${tick}svg\n  ${svg('red')}\n  ${tick}\n`).length).toBe(1);
  expect(drawingsIn(Array.from({ length: 8 }, () => block('red')).join('\n\n')).length).toBe(6);
});

test('the row takes the drawings AI by AI, in order, a few of each', () => {
  expect(rowPictures(answers).map((p) => [p.key, p.name, p.n, p.of, p.svg])).toEqual([
    ['claude/0', 'Claude', 1, 1, drawn('red')],
    ['codex/0', 'Codex', 1, 2, drawn('green')],
    ['codex/1', 'Codex', 2, 2, drawn('blue')],
  ]);
  const many = rowPictures([{ agent: 'claude', name: 'Claude', text: Array.from({ length: 6 }, () => block('red')).join('\n\n') }]);
  expect(many.length).toBe(maxPerAI);
  expect(rowPictures([])).toEqual([]);
});

test("a small picture's page has no script and allows none, and reaches no address", () => {
  const page = svgTilePage(svg('red'));
  expect(page).toContain("script-src 'none'");
  expect(page).toContain("default-src 'none'");
  expect(page).not.toMatch(/<script/i);
  expect(page).not.toMatch(/https?:/i);
  expect(page).toContain(`<div id="m">${drawn('red')}</div>`);
  // It takes the SVG through the same filter as an answer's picture.
  expect(() => svgTilePage('<svg><script>alert(1)</script></svg>')).toThrow();
  expect(() => svgTilePage('<svg><a href="https://example.com"><rect/></a></svg>')).toThrow();
});

test('answers without a drawing show no row', async () => {
  await render(<PictureRow answers={[answers[2], { agent: 'codex', name: 'Codex', text: 'Seoul.' }]} />);
  expect(screen.queryByTestId('picture-row')).toBeNull();
});

test("every AI's drawings lie side by side under its name, and a press opens one large with the others a press away", async () => {
  const saved = mockPhotoSave.saved.length;
  const user = userEvent.setup();
  await render(<PictureRow answers={answers} />);
  expect(screen.getByRole('header', { name: 'Pictures by AI (3)' })).toBeTruthy();
  const tiles = screen.getAllByRole('imagebutton');
  expect(tiles.map((x) => x.props.accessibilityLabel)).toEqual([
    'Picture by Claude, 1 of 3. Opens it large.',
    'Picture by Codex, 2 of 3. Opens it large.',
    'Picture by Codex, 3 of 3. Opens it large.',
  ]);
  // Two drawings of one AI are told apart; one needs no number.
  expect(within(tiles[0]).getByText('Claude')).toBeTruthy();
  expect(within(tiles[1]).getByText('Codex 1')).toBeTruthy();
  expect(within(tiles[2]).getByText('Codex 2')).toBeTruthy();
  // Each small picture is a page without script, in a web view that runs none and takes no touch.
  const webs = screen.getAllByTestId('picture-tile-webview');
  expect(webs.length).toBe(3);
  for (const w of webs) {
    expect(w.props.javaScriptEnabled).toBe(false);
    expect(w.props.allowFileAccess).toBe(false);
    expect(w.props.source.html).toContain("script-src 'none'");
    expect(w.props.source.baseUrl).toBe('about:blank');
    expect(w.props.onShouldStartLoadWithRequest({ url: 'https://example.com/' })).toBe(false);
  }
  expect(webs[1].props.source.html).toContain(drawn('green'));
  expect(screen.queryByTestId('picture-viewer')).toBeNull();

  await user.press(tiles[1]);
  const viewer = screen.getByTestId('picture-viewer');
  expect(within(viewer).getByRole('header', { name: 'Codex 1' })).toBeTruthy();
  expect(within(viewer).getByText('2/3')).toBeTruthy();
  // The large picture is an answer's picture: its code and Save are there.
  expect(within(viewer).getByTestId('svg-block')).toBeTruthy();
  expect(within(viewer).getByTestId('svg-webview').props.source.html).toContain(drawn('green'));
  expect(within(viewer).getByRole('button', { name: 'Show code' })).toBeTruthy();
  expect(within(viewer).getByRole('button', { name: 'Save' })).toBeTruthy();

  await user.press(within(viewer).getByRole('button', { name: 'Next picture' }));
  expect(within(screen.getByTestId('picture-viewer')).getByRole('header', { name: 'Codex 2' })).toBeTruthy();
  expect(within(screen.getByTestId('picture-viewer')).getByText('3/3')).toBeTruthy();
  expect(within(screen.getByTestId('picture-viewer')).getByRole('button', { name: 'Next picture' })).toBeDisabled();
  expect(within(screen.getByTestId('picture-viewer')).getByTestId('svg-webview').props.source.html).toContain(drawn('blue'));
  await user.press(within(screen.getByTestId('picture-viewer')).getByRole('button', { name: 'Previous picture' }));
  await user.press(within(screen.getByTestId('picture-viewer')).getByRole('button', { name: 'Previous picture' }));
  expect(within(screen.getByTestId('picture-viewer')).getByRole('header', { name: 'Claude' })).toBeTruthy();
  expect(within(screen.getByTestId('picture-viewer')).getByRole('button', { name: 'Previous picture' })).toBeDisabled();

  await user.press(within(screen.getByTestId('picture-viewer')).getByRole('button', { name: 'Close' }));
  expect(screen.queryByTestId('picture-viewer')).toBeNull();
  expect(mockPhotoSave.saved.length).toBe(saved);
});

test('one picture opens without a count or arrows', async () => {
  const user = userEvent.setup();
  await render(<PictureRow answers={[answers[0]]} />);
  await user.press(screen.getByRole('imagebutton'));
  const viewer = screen.getByTestId('picture-viewer');
  expect(within(viewer).queryByText('1/1')).toBeNull();
  expect(within(viewer).queryByRole('button', { name: 'Next picture' })).toBeNull();
});

test('more pictures than the row holds are a count that opens the next one', async () => {
  const user = userEvent.setup();
  const four = (agent: string, name: string) => ({ agent, name, text: Array.from({ length: 4 }, () => block('red')).join('\n\n') });
  await render(<PictureRow answers={[four('claude', 'Claude'), four('codex', 'Codex')]} />);
  expect(screen.getByRole('header', { name: 'Pictures by AI (8)' })).toBeTruthy();
  expect(screen.getAllByTestId('picture-tile-webview').length).toBe(maxTiles);
  const more = screen.getByRole('button', { name: '2 more pictures' });
  expect(within(more).getByText('+2')).toBeTruthy();
  await user.press(more);
  expect(within(screen.getByTestId('picture-viewer')).getByText('7/8')).toBeTruthy();
  expect(within(screen.getByTestId('picture-viewer')).getByRole('header', { name: 'Codex 3' })).toBeTruthy();
});

test('the words are Korean in Korean', async () => {
  setLang('ko');
  const user = userEvent.setup();
  await render(<PictureRow answers={answers} />);
  expect(screen.getByRole('header', { name: 'AI별 그림 3개' })).toBeTruthy();
  const tiles = screen.getAllByRole('imagebutton');
  expect(tiles[0].props.accessibilityLabel).toBe('Claude의 그림, 3개 중 1번째. 크게 봅니다.');
  await user.press(tiles[0]);
  expect(screen.getByRole('button', { name: '다음 그림' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '이전 그림' })).toBeTruthy();
});

// On the Ask screen: the row sits between the combined answer and the AIs' cards.
describe('on the Ask screen', () => {
  let agent: FakeAgent;
  beforeEach(() => {
    agent = new FakeAgent();
    agent.enableAsk();
    agent.enableSynth();
    globalThis.fetch = agent.fetch;
  });

  async function asked(replies: Record<string, string>) {
    agent.askReplies = replies;
    const user = userEvent.setup();
    await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
    await renderRouter('./src/app', { initialUrl: '/' });
    await user.type(await screen.findByLabelText('Ask a question'), 'draw a cat');
    await user.press(screen.getByRole('button', { name: 'Ask' }));
    await screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
    return user;
  }

  test('a question whose answers hold drawings shows them by AI under the combined answer', async () => {
    const user = await asked({ claude: answers[0].text, codex: answers[1].text, opencode: answers[2].text });
    await waitFor(() => expect(screen.getByRole('header', { name: 'Pictures by AI (3)' })).toBeTruthy());
    expect(screen.getAllByTestId('picture-tile-webview').length).toBe(3);
    // The cards of the answers are still there, after the row.
    expect(screen.getByRole('header', { name: 'Answers by AI (3)' })).toBeTruthy();
    await user.press(screen.getAllByRole('imagebutton', { name: /^Picture by / })[0]);
    expect(within(screen.getByTestId('picture-viewer')).getByRole('header', { name: 'Claude' })).toBeTruthy();
  });

  test('a question whose answers hold none shows no row', async () => {
    await asked({ claude: 'Seoul.', codex: 'Seoul.', opencode: 'Seoul.' });
    expect(screen.getByRole('header', { name: 'Answers by AI (3)' })).toBeTruthy();
    expect(screen.queryByTestId('picture-row')).toBeNull();
  });
});
