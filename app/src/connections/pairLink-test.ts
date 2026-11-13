import { isOldPairLink, parsePairLink } from './pairLink';

const code = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
const pc = 'askorus-pc.tail1.ts.net';

test('parses the link the PC shows', () => {
  expect(parsePairLink(`askorus://pair?v=2&host=askorus-desktop-x.tail1.ts.net&port=8790&code=${code}`)).toEqual({
    host: 'askorus-desktop-x.tail1.ts.net',
    port: 8790,
    code,
  });
});

test('accepts the code as printed with dashes, and any letter case in the host', () => {
  expect(parsePairLink(`askorus://pair?v=2&host=${pc}&port=8790&code=abcd-efgh-ijkl-mnop`)?.code).toBe('ABCDEFGHIJKLMNOP');
  expect(parsePairLink(`askorus://pair?v=2&host=ASKORUS-PC.Tail1.TS.NET&port=8790&code=${code}`)?.host).toBe(pc);
});

test.each([
  ['empty', ''],
  ['other scheme', `http://${pc}:8790/pair?v=2&code=${code}`],
  ['other app', `otherapp://pair?v=2&host=${pc}&port=8790&code=${code}`],
  ['other action', `askorus://open?v=2&host=${pc}&port=8790&code=${code}`],
  ['newer version', `askorus://pair?v=3&host=${pc}&port=8790&code=${code}`],
  ['no version', `askorus://pair?host=${pc}&port=8790&code=${code}`],
  ['no code', `askorus://pair?v=2&host=${pc}&port=8790`],
  ['short code', `askorus://pair?v=2&host=${pc}&port=8790&code=ABC`],
  ['bad characters in code', `askorus://pair?v=2&host=${pc}&port=8790&code=${code.slice(0, 20)}018!?`],
  ['host with a path', `askorus://pair?v=2&host=evil.com/x.ts.net&port=8790&code=${code}`],
  ['host with a user', `askorus://pair?v=2&host=a@evil.ts.net&port=8790&code=${code}`],
  ['port out of range', `askorus://pair?v=2&host=${pc}&port=70000&code=${code}`],
  ['no port', `askorus://pair?v=2&host=${pc}&code=${code}`],
  // R-94: only a name under ts.net. (Whether it is one of the person's own
  // devices is then asked of the tailnet itself, before anything is sent.)
  ['a host outside ts.net', `askorus://pair?v=2&host=evil.example.org&port=443&code=${code}`],
  ['ts.net in the middle of the name', `askorus://pair?v=2&host=desktop-x.tail1.ts.net.example.org&port=443&code=${code}`],
  ['a name that only ends like ts.net', `askorus://pair?v=2&host=evilts.net&port=443&code=${code}`],
  ['ts.net itself', `askorus://pair?v=2&host=ts.net&port=443&code=${code}`],
  ['a bare name', `askorus://pair?v=2&host=pc&port=8790&code=${code}`],
  ['an address', `askorus://pair?v=2&host=100.64.0.9&port=8790&code=${code}`],
  // #57: the phone-browser test page went with `tailscale serve`.
  ['a test-page link', `https://${pc}:8790/test/#code=${code}`],
])('refuses: %s', (_name, text) => {
  expect(parsePairLink(text)).toBeNull();
});

// #57: a version 1 link names an agent behind `tailscale serve`. The app
// does not try it (it could only fail in a confusing way); it is told apart
// so the screen can say to update the agent.
test('a link of an older agent is refused, and known as such', () => {
  const old = `askorus://pair?v=1&host=desktop-x.tail1.ts.net&port=8790&code=${code}`;
  expect(parsePairLink(old)).toBeNull();
  expect(isOldPairLink(old)).toBe(true);
  expect(isOldPairLink(`askorus://pair?v=2&host=${pc}&port=8790&code=${code}`)).toBe(false);
  expect(isOldPairLink('https://example.org/?v=1')).toBe(false);
  expect(isOldPairLink('not a link')).toBe(false);
});
