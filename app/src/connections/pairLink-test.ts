import { parsePairLink } from './pairLink';

const code = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
const pc = 'pc.tail1.ts.net';

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

test('accepts a lower-case or dashed code, and a host in capitals', () => {
  expect(parsePairLink(`askorus://pair?v=1&host=${pc}&port=8790&code=abcd-efgh-ijkl-mnop`)?.code).toBe('ABCDEFGHIJKLMNOP');
  expect(parsePairLink(`askorus://pair?v=1&host=PC.Tail1.TS.NET&port=8790&code=${code}`)?.host).toBe(pc);
});

test.each([
  ['not a link', 'hello'],
  ['other scheme', `http://${pc}:8790/test/#code=${code}`],
  ['other app', `otherapp://pair?v=1&host=${pc}&port=8790&code=${code}`],
  ['other action', `askorus://open?v=1&host=${pc}&port=8790&code=${code}`],
  ['unknown version', `askorus://pair?v=2&host=${pc}&port=8790&code=${code}`],
  ['no code', `askorus://pair?v=1&host=${pc}&port=8790`],
  ['short code', `askorus://pair?v=1&host=${pc}&port=8790&code=ABC`],
  ['bad characters in code', `askorus://pair?v=1&host=${pc}&port=8790&code=${code.slice(0, 20)}018!?`],
  ['host with a path', `askorus://pair?v=1&host=evil.com/x.ts.net&port=8790&code=${code}`],
  ['host with a user', `askorus://pair?v=1&host=a@evil.ts.net&port=8790&code=${code}`],
  ['port out of range', `askorus://pair?v=1&host=${pc}&port=70000&code=${code}`],
  ['https without the test path', `https://${pc}:8790/#code=${code}`],
  // R-94: only the PC's own name under ts.net; any other server could "allow" the pairing itself.
  ['a host outside ts.net', `askorus://pair?v=1&host=evil.example.org&port=443&code=${code}`],
  ['ts.net in the middle of the name', `askorus://pair?v=1&host=desktop-x.tail1.ts.net.example.org&port=443&code=${code}`],
  ['a name that only ends like ts.net', `askorus://pair?v=1&host=evilts.net&port=443&code=${code}`],
  ['ts.net itself', `askorus://pair?v=1&host=ts.net&port=443&code=${code}`],
  ['a bare name', `askorus://pair?v=1&host=pc&port=8790&code=${code}`],
  ['an address', `askorus://pair?v=1&host=100.64.0.9&port=8790&code=${code}`],
  ['a test-page link outside ts.net', `https://evil.example.org/test/#code=${code}`],
  ['a test-page link with ts.net in the middle', `https://pc.tail1.ts.net.example.org/test/#code=${code}`],
])('rejects %s', (_name, text) => {
  expect(parsePairLink(text)).toBeNull();
});
