// Following an agent update (#35): which outcome a status shows, and the
// asking until the update has ended.

import { AgentClient, AgentError } from './client';
import { followUpdate, outcomeOf } from './selfUpdate';
import type { UpdateStatus } from './types';
import { errorText, setLang } from '@/i18n';

const base: UpdateStatus = { on: true, can: true, allowed: true, current: '0.5.0', latest: '0.5.1', newer: true, running: false, busy: { asks: 0, folders: 0 } };

test('a status shows an update to a version as ended only once it has', () => {
  expect(outcomeOf({ ...base, current: '0.5.1' }, '0.5.1')).toEqual({ kind: 'done', version: '0.5.1' });
  expect(outcomeOf({ ...base, last: { from: '0.5.0', to: '0.5.1', state: 'rolled-back', reason: 'r', at: '' } }, '0.5.1')).toEqual({ kind: 'rolledBack', reason: 'r' });
  expect(outcomeOf({ ...base, last: { from: '0.5.0', to: '0.5.1', state: 'failed', at: '' } }, '0.5.1')?.kind).toBe('failed');
  expect(outcomeOf({ ...base, last: { from: '0.5.0', to: '0.5.1', state: 'not-taken', at: '' } }, '0.5.1')?.kind).toBe('notTaken');
  // Still running: the record is an earlier try's.
  expect(outcomeOf({ ...base, running: true, last: { from: '0.5.0', to: '0.5.1', state: 'not-taken', at: '' } }, '0.5.1')).toBeNull();
  // A record of an update to another version says nothing of this one.
  expect(outcomeOf({ ...base, last: { from: '0.4.0', to: '0.5.0', state: 'rolled-back', at: '' } }, '0.5.1')).toBeNull();
  expect(outcomeOf(base, '0.5.1')).toBeNull();
});

test('followUpdate asks through the wait and the swap until the new agent answers', async () => {
  const answers: (UpdateStatus | Error)[] = [{ ...base, running: true }, new AgentError('agent_away', 503), new Error('network'), { ...base, current: '0.5.1' }];
  const client = { updateStatus: jest.fn(async () => {
    const a = answers.shift()!;
    if (a instanceof Error) throw a;
    return a;
  }) } as unknown as AgentClient;
  const phases: string[] = [];
  const out = await followUpdate(client, '0.5.1', { every: 1, onPhase: (p) => phases.push(p) });
  expect(out).toEqual({ kind: 'done', version: '0.5.1' });
  expect(phases).toEqual(['waiting', 'away', 'away']);
});

test('followUpdate gives up when the PC does not come back in time', async () => {
  let t = 0;
  const client = { updateStatus: jest.fn(async () => { throw new Error('network'); }) } as unknown as AgentClient;
  const out = await followUpdate(client, '0.5.1', { every: 1, limitMs: 10, now: () => (t += 4) });
  expect(out).toEqual({ kind: 'lost' });
});

// R-138 (#35 review): the PC may ask GitHub before it answers, so the
// update requests wait longer than the usual 15 seconds, and the phone
// follows an update for as long as the PC may wait and download.
test('the update requests wait as long as the PC may take', async () => {
  const request = jest.spyOn(AgentClient.prototype, 'request').mockResolvedValue({} as never);
  const c = Object.create(AgentClient.prototype) as AgentClient;
  await c.updateStatus();
  await c.startUpdate('0.5.1');
  expect(request).toHaveBeenNthCalledWith(1, 'GET', 'update', undefined, undefined, 30_000);
  expect(request).toHaveBeenNthCalledWith(2, 'POST', 'update', { version: '0.5.1' }, undefined, 75_000);
  request.mockRestore();

  let t = 0;
  const client = { updateStatus: jest.fn(async () => ({ ...base, running: true })) } as unknown as AgentClient;
  const started = Date.now();
  const out = await followUpdate(client, '0.5.1', { every: 1, now: () => (t += 60_000) });
  expect(out).toEqual({ kind: 'lost' });
  // One ask a minute: about 25 of them before it gives up.
  expect((client.updateStatus as jest.Mock).mock.calls.length).toBeGreaterThanOrEqual(24);
  expect(Date.now() - started).toBeLessThan(5000);
});

test('work refused while the agent updates says so', () => {
  setLang('en');
  expect(errorText(new AgentError('updating', 503))).toBe("The PC's agent is updating. Try again in a minute.");
});
