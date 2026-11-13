import { AgentClient, AgentError, NetworkError } from '../api/client';
import { cause, causesToCheck } from './reach';

test('causes from agent answers', () => {
  expect(cause(new AgentError('unauthorized', 401))).toBe('unpaired');
  expect(cause(new AgentError('wrong_account', 403))).toBe('wrong_account');
  // No proxy stands in front of the agent any more (#57): a 502 is the agent's own.
  expect(cause(new AgentError('http_502', 502))).toBe('other');
  expect(cause(new AgentError('launch_failed', 502))).toBe('other');
  expect(cause(new Error('x'))).toBe('other');
});

test('a fast failure points at the phone first, a slow one at the PC', () => {
  expect(cause(new NetworkError(undefined, false))).toBe('phone_offline');
  expect(cause(new NetworkError(undefined, true))).toBe('pc_unreachable');
  expect(causesToCheck('phone_offline')[0]).toBe('phone_offline');
  expect(causesToCheck('pc_unreachable')[0]).toBe('pc_unreachable');
  expect(causesToCheck('pc_unreachable')).toEqual(['pc_unreachable', 'phone_offline']);
  expect(causesToCheck('unpaired')).toEqual(['unpaired']);
});

test('the client marks a request that ran out of time', async () => {
  const hang = ((_u: RequestInfo | URL, init?: RequestInit) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new Error('aborted')));
    })) as typeof fetch;
  const err = await new AgentClient('https://pc', { fetch: hang, timeoutMs: 10 }).info().catch((e) => e);
  expect(err).toBeInstanceOf(NetworkError);
  expect(err.timedOut).toBe(true);

  const refuse = (async () => {
    throw new TypeError('Network request failed');
  }) as typeof fetch;
  const fast = await new AgentClient('https://pc', { fetch: refuse }).info().catch((e) => e);
  expect(fast.timedOut).toBe(false);
});
