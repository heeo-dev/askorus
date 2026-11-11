// Before a pairing code is sent, the app checks that the PC is inside the
// person's own tailnet (#42). The native module answers on a phone; here a
// lookup is given.

import { type Lookup, type Route, checkTailnetHost, isTailscaleAddress, judgeRoutes } from '@/connections/tailnetCheck';

const HOST = 'desktop-test.tail0.ts.net';
const inTailnet: Route = { address: '100.101.102.103', local: '100.99.1.2', via: 'tun0' };

describe('isTailscaleAddress', () => {
  test('100.64.0.0/10 and nothing next to it', () => {
    for (const a of ['100.64.0.0', '100.64.0.1', '100.100.100.100', '100.127.255.255', ' 100.70.1.2 ']) expect([a, isTailscaleAddress(a)]).toEqual([a, true]);
    for (const a of ['100.63.255.255', '100.128.0.0', '10.0.0.1', '192.168.0.10', '101.64.0.1', '8.8.8.8', '100.64.0', '100.64.0.1.5', '100.64.0.256', '1100.64.0.1']) {
      expect([a, isTailscaleAddress(a)]).toEqual([a, false]);
    }
  });

  test('fd7a:115c:a1e0::/48 and nothing next to it', () => {
    for (const a of ['fd7a:115c:a1e0::1', 'FD7A:115C:A1E0:AB12:4843:CD96:6265:6667', 'fd7a:115c:a1e0:0:0:0:0:1', 'fd7a:115c:a1e0::', 'fd7a:115c:a1e0::53%tun0']) {
      expect([a, isTailscaleAddress(a)]).toEqual([a, true]);
    }
    for (const a of ['fd7a:115c:a1e1::1', 'fd7a:115c::a1e0:1', 'fe80::1', '2001:db8::1', '::1', '::', 'fd7a:115c:a1e0', 'fd7a:115c:a1e0:1:2:3:4:5:6', 'fd7a::115c::a1e0', 'fd7a:115c:a1e0::g']) {
      expect([a, isTailscaleAddress(a)]).toEqual([a, false]);
    }
  });

  test('what is not an address is not one', () => {
    for (const a of ['', 'desktop-test.tail0.ts.net', '100.64.0.1/10', 'localhost', '::ffff:100.64.0.1']) expect([a, isTailscaleAddress(a)]).toEqual([a, false]);
  });
});

describe('judgeRoutes', () => {
  test('every address a Tailscale one, reached from a Tailscale address on a VPN interface: the tailnet', () => {
    expect(judgeRoutes([inTailnet])).toBe('tailnet');
    expect(judgeRoutes([inTailnet, { address: 'fd7a:115c:a1e0::6a01:6667', local: 'fd7a:115c:a1e0::9901:102', via: 'tun1' }])).toBe('tailnet');
  });

  test('no address: the name does not resolve', () => {
    expect(judgeRoutes([])).toBe('unresolved');
  });

  test('a public address is outside, also next to a Tailscale one', () => {
    expect(judgeRoutes([{ address: '203.0.113.7', local: '192.168.0.10', via: 'wlan0' }])).toBe('outside');
    expect(judgeRoutes([inTailnet, { address: '2001:db8::7', local: '', via: '' }])).toBe('outside');
  });

  // Tailscale off, and the network itself hands out 100.x addresses and
  // answers the name: the route does not go through a VPN.
  test('a Tailscale-looking address reached without Tailscale is outside', () => {
    expect(judgeRoutes([{ address: '100.64.0.1', local: '192.168.0.10', via: 'wlan0' }])).toBe('outside');
    expect(judgeRoutes([{ address: '100.64.0.1', local: '100.64.0.5', via: 'wlan0' }])).toBe('outside');
    expect(judgeRoutes([{ address: '100.64.0.1', local: '100.64.0.5', via: 'rmnet_data0' }])).toBe('outside');
  });

  test('what the phone could not tell decides nothing', () => {
    expect(judgeRoutes([{ address: '100.101.102.103', local: '', via: '' }])).toBe('tailnet');
    expect(judgeRoutes([{ address: '100.101.102.103', local: '100.99.1.2', via: '' }])).toBe('tailnet');
  });
});

describe('checkTailnetHost', () => {
  test('asks for the host and port of the link and judges the answer', async () => {
    const lookup = jest.fn<ReturnType<Lookup>, Parameters<Lookup>>(async () => [inTailnet]);
    expect(await checkTailnetHost(HOST, 8443, lookup)).toBe('tailnet');
    expect(lookup).toHaveBeenCalledWith(HOST, 8443);
    expect(await checkTailnetHost(HOST, 443, async () => [])).toBe('unresolved');
    expect(await checkTailnetHost(HOST, 443, async () => [{ address: '203.0.113.7', local: '10.0.0.2', via: 'wlan0' }])).toBe('outside');
  });

  test('without the native module the check is skipped', async () => {
    // jest has no native modules: the default lookup is none.
    expect(await checkTailnetHost(HOST, 443)).toBe('unknown');
    expect(await checkTailnetHost(HOST, 443, null)).toBe('unknown');
  });

  test('a module that fails or answers something else is skipped, with a warning', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    expect(
      await checkTailnetHost(HOST, 443, async () => {
        throw new Error('boom');
      }),
    ).toBe('unknown');
    expect(warn).toHaveBeenCalledTimes(1);
    expect(await checkTailnetHost(HOST, 443, (async () => 'nope') as unknown as Lookup)).toBe('unknown');
    warn.mockRestore();
  });

  test('an answer with fields missing is read as not known, never as a pass for a public address', async () => {
    const odd = (async () => [{ address: '203.0.113.7' }]) as unknown as Lookup;
    expect(await checkTailnetHost(HOST, 443, odd)).toBe('outside');
    const bare = (async () => [{}]) as unknown as Lookup;
    expect(await checkTailnetHost(HOST, 443, bare)).toBe('outside');
  });

  test('no answer in time reads as no address', async () => {
    const never: Lookup = () => new Promise<Route[]>(() => undefined);
    expect(await checkTailnetHost(HOST, 443, never, 20)).toBe('unresolved');
  });
});
