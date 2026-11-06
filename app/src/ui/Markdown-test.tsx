// Answers in Markdown are drawn, not shown as marks (P2-2): **bold** turns
// bold, lists get bullets, tables become rows, and only web links open.

import { getDefaultNormalizer, render, screen, userEvent } from '@testing-library/react-native';
import { Linking, StyleSheet } from 'react-native';

import { setLang } from '@/i18n';

import { Markdown, columnWidths, lex, safeLink } from './Markdown';
import { aiPalettes, palettes, ThemeProvider } from './theme';

const answer = [
  '## 차이',
  '',
  'HTTP는 **암호화하지 않고**, HTTPS는 *TLS*로 암호화합니다. 포트는 `443`입니다.',
  '',
  '- 첫째: **기밀성**',
  '- 둘째',
  '  1. 무결성',
  '  2. 인증',
  '',
  '| 항목 | HTTP | HTTPS |',
  '| --- | --- | --- |',
  '| 암호화 | 없음 | **있음** |',
  '',
  '```',
  'curl https://example.com',
  '```',
  '',
  '> 인용 & 주의',
  '',
  '[출처](https://example.com/a) · [앱](intent://x) · ~~옛말~~',
].join('\n');

const style = (el: { props: { style?: unknown } }) => StyleSheet.flatten(el.props.style as never) ?? {};

test('marks are drawn, not shown', async () => {
  await render(<Markdown text={answer} />);
  expect(screen.queryByText(/\*\*|`|^#|\|/)).toBeNull();
  expect(style(screen.getByText('암호화하지 않고'))).toMatchObject({ fontWeight: '700' });
  expect(style(screen.getByText('TLS'))).toMatchObject({ fontStyle: 'italic' });
  expect(style(screen.getByText('443')).fontFamily).toBeTruthy();
  expect(screen.getByRole('header', { name: '차이' })).toBeTruthy();
  expect(style(screen.getByText('옛말'))).toMatchObject({ textDecorationLine: 'line-through' });
});

test('lists keep bullets and numbers, nested ones too', async () => {
  await render(<Markdown text={answer} />);
  expect(screen.getAllByText('•')).toHaveLength(2);
  expect(screen.getByText('1.')).toBeTruthy();
  expect(screen.getByText('2.')).toBeTruthy();
  expect(screen.getByText('무결성')).toBeTruthy();
});

test('a table becomes rows of cells', async () => {
  await render(<Markdown text={answer} />);
  expect(style(screen.getByText('HTTPS', { exact: true }))).toMatchObject({ fontWeight: '700' }); // header cell
  expect(screen.getByText('없음')).toBeTruthy();
  expect(style(screen.getByText('있음'))).toMatchObject({ fontWeight: '700' });
});

test('every cell of a column gets the same width, so columns line up', async () => {
  const w = columnWidths([
    ['Feature', 'HTTP'],
    ['Server authentication', 'No'],
    ['암호화', 'Yes, with a very very very long explanation that wraps'],
  ]);
  expect(w[0]).toBe(Math.round(21 * 7.2) + 22); // the longest cell of column 1
  expect(w[1]).toBe(240); // capped; the text wraps
  expect(columnWidths([['a', '']])).toEqual([80, 80]); // never too thin
  await render(<Markdown text={answer} />);
  const widths = ['항목', '암호화'].map((t) => style(screen.getByText(t)).width);
  expect(widths[0]).toBe(widths[1]);
});

test('code blocks keep their text; entities read as characters', async () => {
  await render(<Markdown text={answer} />);
  expect(screen.getByText('curl https://example.com')).toBeTruthy();
  expect(screen.getByText('인용 & 주의')).toBeTruthy();
});

test('only web links open', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<Markdown text={answer} />);
  await user.press(screen.getByText('출처'));
  expect(open).toHaveBeenCalledWith('https://example.com/a');
  expect(screen.getByText('앱').props.onPress).toBeUndefined();
  expect(safeLink('javascript:alert(1)')).toBeNull();
  expect(safeLink('HTTP://x.org')).toBe('HTTP://x.org');
  open.mockRestore();
});

test('footnotes: numbers in the text in order of first use, the notes at the end', async () => {
  const text = [
    '먼저 이것[^note], 그다음 저것[^1]. 정의 없는 것[^x].',
    '',
    '[^1]: 각주 내용입니다.',
    '[^note]: 두 번째 각주',
    '    이어지는 줄',
  ].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.queryByText(/\[\^(1|note)\]/)).toBeNull();
  expect(screen.getByText('¹')).toHaveStyle({ fontWeight: '700' }); // [^note] is used first
  expect(screen.getByText('²')).toBeTruthy();
  expect(screen.getByText(/\[\^x\]/)).toBeTruthy(); // no note for it: shown as written
  const notes = screen.getByLabelText('Footnotes');
  expect(notes).toHaveTextContent('¹ 두 번째 각주\n이어지는 줄² 각주 내용입니다.');
});

test('<details> opens and closes; its inside is Markdown too', async () => {
  const user = userEvent.setup();
  const text = ['<details>', '<summary>펼치기</summary>', '', '숨겨진 **내용**입니다.', '', '- 목록', '', '</details>', '', '뒤 문단'].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getByText('▸ 펼치기')).toBeTruthy();
  expect(screen.queryByText('내용')).toBeNull();
  expect(screen.getByText('뒤 문단')).toBeTruthy();
  expect(screen.queryByText(/<\/?details>|<summary>/)).toBeNull();
  await user.press(screen.getByRole('button', { name: '▸ 펼치기' }));
  expect(screen.getByText('내용')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText('목록')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '▾ 펼치기' }));
  expect(screen.queryByText('내용')).toBeNull();
});

test('<details> written without blank lines, as the AI often does', async () => {
  const user = userEvent.setup();
  await render(<Markdown text={'<details>\n<summary>펼치기</summary>\n숨겨진 내용입니다.\n</details>'} />);
  expect(screen.queryByText('숨겨진 내용입니다.')).toBeNull();
  await user.press(screen.getByText('▸ 펼치기'));
  expect(screen.getByText('숨겨진 내용입니다.')).toBeTruthy();
});

test('<br> breaks a line and styling tags drop out; other HTML stays as written', async () => {
  await render(<Markdown text={'| a | b |\n|---|---|\n| 한 줄<br>두 줄 | x |\n\n이건 <b>굵게</b> 쓴 것, 저건 List<T> 그대로.'} />);
  expect(screen.getByText('한 줄\n두 줄')).toBeTruthy();
  expect(screen.queryByText(/<b>|<\/b>/)).toBeNull();
  expect(screen.getByText(/List<T> 그대로/)).toBeTruthy();
});

test('a single ~ is a range, not a strike-through (R-22)', async () => {
  await render(<Markdown text={'수명은 1~10분이고, 재시도는 2~3초 뒤에 합니다. ~~옛말~~'} />);
  expect(screen.getByText(/수명은 1~10분이고, 재시도는 2~3초 뒤에 합니다\./)).not.toHaveStyle({ textDecorationLine: 'line-through' });
  expect(screen.queryByText('10분이고, 재시도는 2')).toBeNull();
  expect(screen.getByText('옛말')).toHaveStyle({ textDecorationLine: 'line-through' });
});

test('link definitions are not shown; the reference opens their address', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<Markdown text={'근거가 있습니다 [1].\n\n[1]: https://a.example/doc "A"\n[2]: <https://b.example>'} />);
  expect(screen.queryByText(/\]:|https:\/\//)).toBeNull();
  await user.press(screen.getByText('1'));
  expect(open).toHaveBeenCalledWith('https://a.example/doc');
  open.mockRestore();
});

test('HTML comments are hidden; entities read as characters, but not in code', async () => {
  const text = [
    '앞<!-- 숨김 -->뒤',
    '',
    '<!-- 블록 주석 -->',
    '',
    '<!-- 같은 줄 -->남은 **글**',
    '',
    '가&nbsp;나 &copy; &mdash; &#8212; &#x2014; &bogus; a & b &amp; `&lt;b&gt;`',
  ].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.queryByText(/<!--|-->|숨김|주석|같은 줄/)).toBeNull();
  expect(screen.getByText('앞뒤')).toBeTruthy();
  expect(screen.getByText('글')).toHaveStyle({ fontWeight: '700' });
  const asIs = { normalizer: getDefaultNormalizer({ collapseWhitespace: false }) }; // keep the no-break space
  expect(screen.getByText(/^가 나 © — — — &bogus; a & b & /, asIs)).toBeTruthy();
  expect(screen.getByText('&lt;b&gt;')).toBeTruthy();
});

test('<a href> is a link (web addresses only); a lone <a> tag goes', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  const text = '<a href="https://example.com/?a=1&amp;b=2">링크</a>, <a href=\'javascript:x\'>나쁜</a>, <a name="x">앵커</a>, <a href="https://x.org">닫지 않음';
  await render(<Markdown text={text} />);
  expect(screen.queryByText(/<\/?a/)).toBeNull();
  await user.press(screen.getByText('링크'));
  expect(open).toHaveBeenCalledWith('https://example.com/?a=1&b=2');
  expect(screen.getByText('나쁜').props.onPress).toBeUndefined();
  expect(screen.getByText(/앵커, 닫지 않음$/)).toBeTruthy();
  open.mockRestore();
});

test('math in a line reads as Unicode; money stays money', async () => {
  const text = [
    '넓이는 $\\pi r^2$이고 \\(\\alpha + \\beta\\)도 있습니다.',
    '',
    '가격은 $5에서 $10 사이, 근거 \\[1\\], 괄호 \\(주의\\), US$5-$10.',
    '',
    '못 그리는 식 $\\begin{matrix}a\\end{matrix}$',
  ].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getByText('넓이는 πr²이고 α + β도 있습니다.')).toBeTruthy();
  expect(screen.getByText('가격은 $5에서 $10 사이, 근거 [1], 괄호 (주의), US$5-$10.')).toBeTruthy();
  expect(style(screen.getByText('\\begin{matrix}a\\end{matrix}')).fontFamily).toBeTruthy(); // shown as TeX, in code style
  expect(screen.queryByTestId('math-webview')).toBeNull();
});

test('a formula on lines of its own is drawn by KaTeX', async () => {
  const text = ['근의 공식:', '$$', 'x = \\frac{-b \\pm \\sqrt{b^2-4ac}}{2a}', '$$', '그리고', '', '\\[', 'E = mc^2', '\\]', '', '$$a$$ 와 $$b^2$$ 는 줄 안에 있습니다.'].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getAllByTestId('math-webview')).toHaveLength(2);
  expect(screen.getByText('근의 공식:')).toBeTruthy();
  expect(screen.getByText('그리고')).toBeTruthy();
  expect(screen.getByText('a 와 b² 는 줄 안에 있습니다.')).toBeTruthy();
  expect(screen.queryByText(/\$\$|\\\[|\\frac/)).toBeNull();
});

test('math as the three AIs write it (10-30 answers)', async () => {
  const claude = [
    '$a \\neq 0$인 이차방정식 $ax^2 + bx + c = 0$을 풉니다.',
    '',
    '1. 양변을 $a$로 나눕니다.',
    '$$x^2 + \\frac{b}{a}x + \\frac{c}{a} = 0$$',
    '',
    '2. 양변에 $\\left(\\frac{b}{2a}\\right)^2$을 더합니다.',
    '$$\\left(x + \\frac{b}{2a}\\right)^2 = \\frac{b^2 - 4ac}{4a^2}$$',
  ].join('\n');
  const codex = [
    '이차방정식 \\(ax^2+bx+c=0\\)에서 \\(a\\neq0\\)이라 하고,',
    '',
    '\\[',
    '\\boxed{x=\\frac{-b\\pm\\sqrt{b^2-4ac}}{2a}}',
    '\\]',
  ].join('\n');
  const opencode = '$$f(x)=\\frac{1}{\\sigma\\sqrt{2\\pi}}\\exp\\!\\left(-\\frac{(x-\\mu)^2}{2\\sigma^2}\\right),\\quad -\\infty<x<\\infty$$';
  await render(<Markdown text={[claude, codex, opencode].join('\n\n')} />);
  expect(screen.getAllByTestId('math-webview')).toHaveLength(4);
  expect(screen.getByText('a ≠ 0인 이차방정식 ax² + bx + c = 0을 풉니다.')).toBeTruthy();
  expect(screen.getByText('양변에 (b/(2a))²을 더합니다.')).toBeTruthy();
  expect(screen.getByText('이차방정식 ax²+bx+c=0에서 a≠0이라 하고,')).toBeTruthy();
  expect(screen.queryByText(/\$|\\\(|\\\[|\\frac/)).toBeNull();
});

// Found in the 10-30 review of the math change (each case was reproduced first).
const drawnText = (el: unknown): string =>
  typeof el === 'string'
    ? el
    : Array.isArray(el)
      ? el.map(drawnText).join('')
      : el && typeof el === 'object' && 'children' in el
        ? drawnText(el.children)
        : '';

test('$ for money or in code is not math', async () => {
  const lines = [
    'Pro 플랜은 $20이고, 결제 후 `$HOME/.config`에 키가 저장됩니다.',
    '요금은 월 $20입니다(단위: US$).',
    '월 $20(US$ 기준, 부가세 별도)',
    'costs $5 or five dollars ($).',
    '가격은 5$와 10$입니다.',
    "Katz's ($$) and Le Bernardin ($$$$).",
    'Price: $$–$$$ (moderate to expensive).',
    'Use $$ for the PID; in a subshell $$ stays the same.',
  ];
  await render(<Markdown text={lines.join('\n\n')} />);
  for (const line of lines.slice(1)) expect(screen.getByText(line)).toBeTruthy();
  expect(screen.getByText(/^Pro 플랜은 \$20이고, 결제 후 \$HOME\/\.config에/)).toBeTruthy();
  expect(style(screen.getByText('$HOME/.config')).fontFamily).toBeTruthy(); // still a code span
});

test('emphasis around math keeps the math whole', async () => {
  await render(<Markdown text={'*최적값은 $f(x^*)$ 이다.*'} />);
  const italic = screen.getByText(/^최적값은 f\(x.*\) 이다\.$/);
  expect(italic).toHaveStyle({ fontStyle: 'italic' });
  expect(screen.queryByText(/\$/)).toBeNull();
});

// R-77 (S23, 11-06): '…25~50만 원)**를' stayed as marks. CommonMark lets
// "**" close after punctuation only before a space or punctuation, and
// open before punctuation only after one; a Korean particle or word sits
// right there. Next to Korean, Chinese or Japanese letters the marks work.
test('bold works next to Korean letters across punctuation; elsewhere CommonMark holds (R-77)', async () => {
  const cases: [string, string][] = [
    ['**RAM을 늘린 Tiny PC(M75q 등, 25~50만 원)**를 1순위로 추천합니다.', 'RAM을 늘린 Tiny PC(M75q 등, 25~50만 원)'],
    ['값은**(괄호 안)**입니다.', '(괄호 안)'],
    ['**"맥미니 M4"**는 비쌉니다.', '"맥미니 M4"'],
    ['가격은 **50만 원.**이라고 했다.', '50만 원.'],
    ['先に**「確認」**してください。', '「確認」'],
  ];
  for (const [text, bold] of cases) {
    const r = await render(<Markdown text={text} />);
    expect(style(screen.getByText(bold))).toMatchObject({ fontWeight: '700' });
    expect(screen.queryByText(/\*\*/)).toBeNull();
    await r.unmount();
  }
  // Latin letters keep the CommonMark rule; a lone * between numbers is no mark.
  for (const text of ['a**(b)**c', 'foo**"bar"**baz', '2*(3+4)*5']) {
    const r = await render(<Markdown text={text} />);
    expect(screen.getByText(text)).toBeTruthy();
    await r.unmount();
  }
});

test('escaped brackets around words stay brackets', async () => {
  await render(<Markdown text={'Usage: cmd \\[options\\] \\[file\\], \\[TODO\\] 정리, See \\[Smith 2020\\]\n\n\\[A\\]'} />);
  expect(screen.getByText('Usage: cmd [options] [file], [TODO] 정리, See [Smith 2020]')).toBeTruthy();
  expect(screen.getByText('[A]')).toBeTruthy();
  expect(screen.queryByTestId('math-webview')).toBeNull();
});

test('an unknown entity named like an object property stays as written', async () => {
  await render(<Markdown text={'a &constructor; b &toString; c'} />);
  expect(screen.getByText('a &constructor; b &toString; c')).toBeTruthy();
});

test('nested <details> with blank lines close at their own </details>', async () => {
  const user = userEvent.setup();
  const text = ['<details>', '<summary>바깥</summary>', '', '<details>', '<summary>안</summary>', '', '안 내용', '', '</details>', '', '바깥 뒤', '', '</details>', '', '끝'].join(
    '\n',
  );
  await render(<Markdown text={text} />);
  expect(screen.queryByText('바깥 뒤')).toBeNull(); // inside the closed outer box
  expect(screen.getByText('끝')).toBeTruthy();
  expect(screen.queryByText(/<\/?details>/)).toBeNull();
  await user.press(screen.getByText('▸ 바깥'));
  expect(screen.getByText('바깥 뒤')).toBeTruthy();
  await user.press(screen.getByText('▸ 안'));
  expect(screen.getByText('안 내용')).toBeTruthy();
});

test('inside a tight <details>, footnotes and [1] links work', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  const text = ['<details>', '<summary>출처</summary>', '본문[^1], 근거 [1]', '[^1]: 각주 내용', '</details>', '', '[1]: https://a.example/doc'].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getByLabelText('Footnotes')).toHaveTextContent('¹ 각주 내용');
  await user.press(screen.getByText('▸ 출처'));
  expect(screen.getByText('¹')).toBeTruthy();
  await user.press(screen.getByText('1'));
  expect(open).toHaveBeenCalledWith('https://a.example/doc');
  open.mockRestore();
});

test('styling HTML on lines of its own reads like a paragraph', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<Markdown text={'첫 문단\n\n<br>\n\n<a href="https://x.example">\n링크\n</a>\n\n<b>\n굵게\n</b>'} />);
  expect(screen.queryByText(/<\/?(br|a|b)\b/)).toBeNull();
  await user.press(screen.getByText(/링크/));
  expect(open).toHaveBeenCalledWith('https://x.example');
  expect(screen.getByText(/굵게/)).toBeTruthy();
  open.mockRestore();
});

test('the details and footnotes words follow the app language', async () => {
  setLang('ko');
  try {
    await render(<Markdown text={'<details>\n내용만\n</details>\n\n본문[^1]\n\n[^1]: 노트'} />);
    expect(screen.getByText('▸ 자세히')).toBeTruthy();
    expect(screen.getByLabelText('각주')).toBeTruthy();
  } finally {
    setLang('en');
  }
});

test('deep nesting is drawn flat instead of overflowing the stack', async () => {
  const caught = jest.spyOn(console, 'error').mockImplementation(() => undefined);
  // drawn, flat past the cap, without falling back to plain text
  for (const text of ['- '.repeat(500) + 'a', '>'.repeat(400) + ' x', '<details>'.repeat(3000), '<details></details>'.repeat(3000), '**'.repeat(300) + 'a' + '**'.repeat(300)]) {
    await render(<Markdown text={text} />);
    expect(drawnText(screen.toJSON()).length).toBeGreaterThan(0);
  }
  expect(caught).not.toHaveBeenCalled();
  // marked itself overflows on these (how deep depends on the engine); the answer is then plain text
  for (const text of ['>'.repeat(3000) + ' x', '**'.repeat(1200) + 'a' + '**'.repeat(1200)]) {
    await render(<Markdown text={text} />);
    expect(drawnText(screen.toJSON()).length).toBeGreaterThan(0);
  }
  caught.mockRestore();
});

test('lexing stays quick on long hostile answers', () => {
  const nested = (k: number) => '<div>\n' + '<!-'.repeat(k) + '<!-- x -->' + '- y -->'.repeat(k) + '\n';
  for (const text of [
    '$'.repeat(50_000),
    '$$a\n'.repeat(12_500),
    '\\[a\n'.repeat(12_500),
    'x\n\n'.repeat(16_000) + '[^1]: n',
    nested(2000),
    '# h\nx\n'.repeat(8000),
    'a\n[^1]: b\n'.repeat(5000),
    '$$a\n# h\n'.repeat(6000),
    '<b>\n' + '<a '.repeat(16_000),
    '<div>\n' + '<!-- '.repeat(10_000),
    'cmd [options] '.repeat(3500),
    'cmd \\[options\\] '.repeat(3000),
  ]) {
    const t0 = performance.now();
    lex(text);
    expect([text.slice(0, 8), performance.now() - t0 < 2000]).toEqual([text.slice(0, 8), true]);
  }
});

test('many lone <a> tags draw quickly', async () => {
  const t0 = performance.now();
  await render(<Markdown text={'x ' + '<a>'.repeat(30_000)} />);
  expect(performance.now() - t0).toBeLessThan(5000);
});

test('one answer gets 40 formulas drawn by KaTeX; the rest show as written', async () => {
  await render(<Markdown text={Array.from({ length: 45 }, (_, i) => `$$x_{${i}} = ${i}$$`).join('\n\n')} />);
  expect(screen.getAllByTestId('math-webview')).toHaveLength(40);
  expect(screen.getByText('x_{44} = 44')).toBeTruthy();
});

// Found in the second check of the fixes (10-30).
test('bold and italics after `code` in the same line are drawn (R-27)', async () => {
  await render(<Markdown text={'`npm i`로 설치한 뒤 **재시작**하세요.\n\na `x` *b* c'} />);
  expect(screen.getByText('재시작')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText('b')).toHaveStyle({ fontStyle: 'italic' });
  expect(screen.queryByText(/\*/)).toBeNull();
});

test('a list of <details> one after another, as answers write questions', async () => {
  const user = userEvent.setup();
  const faq = ['<details>', '<summary>질문 1</summary>', '', '답 1', '', '</details>', '<details>', '<summary>질문 2</summary>', '', '답 2', '', '</details>', '', '끝'];
  await render(<Markdown text={faq.join('\n')} />);
  expect(screen.queryByText(/答|<\/?details>|답 2/)).toBeNull();
  await user.press(screen.getByText('▸ 질문 2'));
  expect(screen.getByText('답 2')).toBeTruthy();
  const many = Array.from({ length: 16 }, (_, i) => `<details><summary>Q${i}</summary>A${i}</details>`).join('\n');
  await render(<Markdown text={many} />);
  expect(screen.getAllByRole('button')).toHaveLength(16);
  expect(screen.queryByText(/<details>/)).toBeNull();
});

test('<details> tags in code or comments inside a box are only text', async () => {
  const user = userEvent.setup();
  const text = ['<details>', '<summary>HTML 팁</summary>', '', '`<details>`로 접고 `</details>`로 닫습니다.', '', '<!-- </details> -->', '', '</details>', '', '끝'].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getByText('끝')).toBeTruthy();
  expect(screen.queryByText(/로 닫습니다/)).toBeNull(); // still inside the closed box
  await user.press(screen.getByText('▸ HTML 팁'));
  expect(screen.getByText('</details>')).toBeTruthy(); // the code span
});

test('<details open> starts open; a blank summary gets the default word; odd closing tags do not repeat', async () => {
  await render(<Markdown text={'<details open>\n<summary>&nbsp;</summary>\n\n펼친 내용\n\n</details>'} />);
  expect(screen.getByText('펼친 내용')).toBeTruthy();
  expect(screen.getByText('▾ Details')).toBeTruthy();
  await render(<Markdown text={'<details>\n<summary>S</summary>\n\n앞 문장 </details\n뒤 문장'} />);
  expect(screen.getAllByRole('button')).toHaveLength(1);
});

test('a comment right before <details> on the same line', async () => {
  const user = userEvent.setup();
  await render(<Markdown text={'<!-- c --><details><summary>S</summary>\n\n안쪽\n\n</details>\n\n끝'} />);
  expect(screen.queryByText(/안쪽|<\/details>/)).toBeNull();
  await user.press(screen.getByText('▸ S'));
  expect(screen.getByText('안쪽')).toBeTruthy();
});

test('\\( \\) and \\[ \\] math inside italics keeps the italics whole', async () => {
  await render(<Markdown text={'*최적값은 \\(f(x^*)\\) 이다.*'} />);
  expect(screen.getByText('최적값은 f(x*) 이다.')).toHaveStyle({ fontStyle: 'italic' });
});

test('more $ that is not math, and math that still is', async () => {
  const notMath = [
    'Price: $$/$$$ (moderate to expensive).',
    'Tiers: $$=moderate,$$$=expensive',
    'costs $20 (in HK$).',
    'Basic is $10 and Pro is 20$.',
    'Continue? \\[Y/n\\] or \\[--force|--dry-run\\], name \\(optional\\)',
  ];
  await render(<Markdown text={notMath.join('\n\n')} />);
  for (const line of notMath.slice(0, 4)) expect(screen.getByText(line)).toBeTruthy();
  expect(screen.getByText('Continue? [Y/n] or [--force|--dry-run], name (optional)')).toBeTruthy();
  await render(<Markdown text={'Set **$HOME** and **$PATH** first.'} />);
  expect(screen.getByText('$HOME')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText('$PATH')).toHaveStyle({ fontWeight: '700' });
  await render(<Markdown text={'넓이 $2 \\times \\text{밑변}$, 지표 $GDP_US$, 식 \\(a\\neq0\\), 근 \\(x\\)'} />);
  expect(screen.queryByText(/\$|\\/)).toBeNull();
});

test('display \\[ on its own lines is a formula even when plain, and keeps \\\\[4pt]', async () => {
  const text = ['\\[', 'a - b', '\\]', '', '\\[', '\\begin{aligned} a &= b \\\\[4pt] c &= d \\end{aligned}', '\\]', '', '$$ 는 현재 셸의 PID입니다. 예: echo $$'].join('\n');
  await render(<Markdown text={text} />);
  expect(screen.getAllByTestId('math-webview')).toHaveLength(2);
  expect(screen.getByText('$$ 는 현재 셸의 PID입니다. 예: echo $$')).toBeTruthy();
});

test('a loose task list shows only its boxes; a footnote defined twice is listed once', async () => {
  await render(<Markdown text={'- [ ] 첫째 할 일\n\n- [x] 둘째 할 일\n\n글[^1]\n\n[^1]: 하나\n\n[^1]: 또 하나'} />);
  expect(screen.getByText('☐')).toBeTruthy();
  expect(screen.getByText('첫째 할 일')).toBeTruthy();
  expect(screen.queryByText(/\[[ x]\]/)).toBeNull();
  expect(screen.getByLabelText('Footnotes')).toHaveTextContent('¹ 하나');
  expect(screen.queryByText(/또 하나/)).toBeNull();
  await render(<Markdown text={'본문[^1]\n\n  [^1]: 들여 쓴 각주'} />);
  expect(screen.getByLabelText('Footnotes')).toHaveTextContent('¹ 들여 쓴 각주');
});

test('a lone <a> does not swallow the next link', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<Markdown text={'x <a> y <a href="https://b.example">B</a> z'} />);
  await user.press(screen.getByText('B'));
  expect(open).toHaveBeenCalledWith('https://b.example');
  open.mockRestore();
});

test('formulas shown as written do not use up the KaTeX budget', async () => {
  const text = ['$$' + 'x+'.repeat(2000) + 'x$$', ...Array.from({ length: 40 }, (_, i) => `$$y_{${i}}$$`)].join('\n\n');
  await render(<Markdown text={text} />);
  expect(screen.getAllByTestId('math-webview')).toHaveLength(40);
});

test('past 50,000 characters or 1,000 emphasis marks the rest of an answer is plain text', async () => {
  await render(<Markdown text={'**앞**\n' + 'x\n'.repeat(26_000) + '**뒤**'} />);
  expect(screen.getByText('앞')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText(/\*\*뒤\*\*$/)).toBeTruthy();
  // marked reads to the end for every "*" that opens nothing: keep its work small
  const t0 = performance.now();
  await render(<Markdown text={'**앞**\n' + '*a '.repeat(16_000)} />);
  expect(performance.now() - t0).toBeLessThan(3000);
  expect(screen.getByText('앞')).toHaveStyle({ fontWeight: '700' });
});

test('plain text stays as it is', async () => {
  await render(<Markdown text={'서울입니다.'} />);
  expect(screen.getByText('서울입니다.')).toBeTruthy();
});

test('#6: the AIs a combined answer names are chips in their own colours, plain with AI colours off; the subject stays text', async () => {
  const text = '{{Claude}}가 48~49개, {{OpenCode}}가 49개라고 합니다. Claude Code의 스킬은 따로 셉니다. {{Gemini}}는 답하지 않았습니다.';
  const chip = (name: string) => StyleSheet.flatten(screen.getByText(` ${name} `).props.style);
  await render(<Markdown text={text} size="synth" ais={['claude', 'opencode', 'mistral']} />);
  expect(chip('Claude')).toMatchObject({ fontWeight: '600', backgroundColor: aiPalettes.light.sky.bg, color: aiPalettes.light.sky.fg });
  expect(chip('OpenCode')).toMatchObject({ backgroundColor: aiPalettes.light.teal.bg, color: aiPalettes.light.teal.fg });
  expect(screen.getByText(/Claude Code의 스킬/)).toBeTruthy();
  expect(screen.queryByText(' Gemini ')).toBeNull(); // did not answer: plain text without braces

  const kv = { get: async () => '{"theme":"dark","aiColours":false}', set: async () => {}, remove: async () => {} };
  await render(
    <ThemeProvider kv={kv}>
      <Markdown text={text} size="synth" ais={['claude', 'opencode']} />
    </ThemeProvider>,
  );
  expect(chip('Claude')).toMatchObject({ backgroundColor: palettes.dark.avatar, color: palettes.dark.text });
  expect(chip('OpenCode')).toMatchObject({ backgroundColor: palettes.dark.avatar, color: palettes.dark.text });
});
