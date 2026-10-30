// Connection checks for one PC, in the order things break: can the phone
// reach the PC, does the PC still know this phone, is Claude Code there.

import type { AgentClient } from '../api/client';
import { errorText, has, type MessageKey, t } from '../i18n';
import { cause, causesToCheck } from './reach';

const checkKey: Record<string, MessageKey> = {
  phone_offline: 'reach.check.phone',
  pc_unreachable: 'reach.check.pc',
  agent_down: 'reach.check.agent',
};

/** note: nothing is wrong, but something is missing (an optional AI). */
export type Check = { ok: boolean; text: string; note?: boolean };

// Display names of the AIs the agent may report. Claude Code runs the
// remote sessions; the others only answer questions for now (Phase 2), so
// a missing one is a note, not a failure.
const agentNames: Record<string, string> = { claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' };

// The API version this app speaks; a PC on an older one may lack features.
const apiVersion = 1;

export async function diagnose(client: AgentClient): Promise<Check[]> {
  let info;
  try {
    info = await client.info();
  } catch (e) {
    const c = cause(e);
    if (c === 'phone_offline' || c === 'pc_unreachable' || c === 'agent_down') {
      return [
        { ok: false, text: t(c === 'agent_down' ? 'reach.agent_down' : 'reach.offline') },
        ...causesToCheck(c).map((x) => ({ ok: false, text: t(checkKey[x]) })),
      ];
    }
    if (c === 'unpaired' || c === 'wrong_account') {
      return [
        { ok: true, text: t('diag.reached') },
        { ok: false, text: errorText(e) },
      ];
    }
    return [{ ok: false, text: errorText(e) }];
  }

  const out: Check[] = [
    { ok: true, text: t('diag.reached') },
    { ok: true, text: t('diag.paired') },
    info.apiVersion >= apiVersion
      ? { ok: true, text: t('diag.agent', { version: info.version }) }
      : { ok: false, text: t('diag.agentOld', { version: info.version }) },
  ];
  for (const a of info.agents) {
    const name = agentNames[a.agent] ?? a.agent;
    if (a.agent === 'claude') {
      out.push(
        a.available
          ? { ok: true, text: t('diag.claude', { version: a.version ?? '?' }) }
          : { ok: false, text: t('error.agent_unavailable') },
      );
    } else if (a.available) {
      out.push({ ok: true, text: t('diag.askAgent', { name, version: a.version ?? '?' }) });
    } else {
      // Not installed, or there but not working (only an npm script, or
      // its version check failed): a note either way, with the reason.
      const notices = a.notices ?? [];
      const missing = notices.every((n) => n === `${a.agent}_not_found`);
      out.push({ ok: true, note: true, text: t(missing ? 'diag.askAgentMissing' : 'diag.askAgentBroken', { name }) });
      for (const n of missing ? [] : notices) {
        const key = `notice.${n}`;
        out.push({ ok: true, note: true, text: has(key) ? t(key) : n });
      }
      continue;
    }
    for (const n of a.notices ?? []) {
      const key = `notice.${n}`;
      out.push({ ok: false, text: has(key) ? t(key) : n });
    }
  }
  return out;
}
