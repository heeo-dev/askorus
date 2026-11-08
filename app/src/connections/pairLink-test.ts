import { parsePairLink } from './pairLink';

const code = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';

test('reads the app link from the PC QR', () => {
  expect(parsePairLink(`askorus://pair?v=1&host=desktop-x.tail1.ts.net&port=8790&code=${code}`)).toEqual({
    host: 'desktop-x.tail1.ts.net',
    port: 8790,
    code,
  });
});

test('reads the test-page link (pair -test-page)', () => {
  expect(parsePairLink(`https://desktop-x.tail1.ts.net:8790/test/#code=${code}`)).toEqual({
    host: 'desktop-x.tail1.ts.net',
    port: 8790,
    code,
  });
  expect(parsePairLink(`https://desktop-x.tail1.ts.net/test/#code=${code}`)?.port).toBe(443);
});

test('accepts a lower-case or dashed code', () => {
  expect(parsePairLink(`askorus://pair?v=1&host=pc&port=8790&code=abcd-efgh-ijkl-mnop`)?.code).toBe(
    'ABCDEFGHIJKLMNOP',
  );
});

test.each([
  ['not a link', 'hello'],
  ['other scheme', `http://pc:8790/test/#code=${code}`],
  ['other app', `otherapp://pair?v=1&host=pc&port=8790&code=${code}`],
  ['other action', `askorus://open?v=1&host=pc&port=8790&code=${code}`],
  ['unknown version', `askorus://pair?v=2&host=pc&port=8790&code=${code}`],
  ['no code', 'askorus://pair?v=1&host=pc&port=8790'],
  ['short code', 'askorus://pair?v=1&host=pc&port=8790&code=ABC'],
  ['bad characters in code', `askorus://pair?v=1&host=pc&port=8790&code=${code.slice(0, 20)}018!?`],
  ['host with a path', `askorus://pair?v=1&host=evil.com/x&port=8790&code=${code}`],
  ['host with a user', `askorus://pair?v=1&host=a@evil.com&port=8790&code=${code}`],
  ['port out of range', `askorus://pair?v=1&host=pc&port=70000&code=${code}`],
  ['https without the test path', `https://pc.ts.net:8790/#code=${code}`],
])('rejects %s', (_name, text) => {
  expect(parsePairLink(text)).toBeNull();
});
