// An agent older than the app (11-18): the versions it compares and the
// line it shows for the PC's system.

import { agentBehind, installLine, parseVersion, updateLine } from './agentUpdate';

test('a version is major.minor.patch at the start; a build suffix is left out', () => {
  expect(parseVersion('0.3.3')).toEqual([0, 3, 3]);
  expect(parseVersion('v0.10.2')).toEqual([0, 10, 2]);
  expect(parseVersion('0.4.0-dev+f0a576d')).toEqual([0, 4, 0]);
  expect(parseVersion(' 1.2.3 ')).toEqual([1, 2, 3]);
  for (const v of ['', 'dev', '0.4', 'x0.4.0', undefined, null]) expect(parseVersion(v)).toBeNull();
});

test('only an agent older than the app is behind; unknown versions never are', () => {
  expect(agentBehind('0.3.3', '0.4.0')).toBe(true);
  expect(agentBehind('0.3.4', '0.4.0')).toBe(true); // 0.3.4 is a GitHub release only, 0.4.0 comes from the store too
  expect(agentBehind('0.3.9', '0.3.10')).toBe(true); // numbers, not text
  expect(agentBehind('0.9.0', '1.0.0')).toBe(true);
  expect(agentBehind('0.4.0', '0.4.0')).toBe(false);
  expect(agentBehind('0.4.1', '0.4.0')).toBe(false);
  // A build of the branch carries the version it is heading for.
  expect(agentBehind('0.4.0-dev+f0a576d', '0.4.0')).toBe(false);
  // Under tests and in a build run by hand the app has no version.
  expect(agentBehind('0.3.3', '')).toBe(false);
  expect(agentBehind('dev', '0.4.0')).toBe(false);
  expect(agentBehind(undefined, '0.4.0')).toBe(false);
});

test('Windows gets the PowerShell line; macOS, Linux and an unknown system the shell line', () => {
  expect(updateLine('windows')).toEqual({ shell: 'powershell', line: installLine.windows });
  for (const os of ['darwin', 'linux', '']) expect(updateLine(os)).toEqual({ shell: 'terminal', line: installLine.unix });
  expect(installLine.windows).toBe('irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 | iex');
  expect(installLine.unix).toBe('curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh | sh');
});
