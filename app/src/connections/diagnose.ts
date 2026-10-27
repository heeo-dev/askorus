// Connection checks for one PC, in the order things break: can the phone
// reach the PC, does the PC still know this phone, is Claude Code there.

import { AgentError, type AgentClient, NetworkError } from '../api/client';
import { errorText, has, t } from '../i18n';

export type Check = { ok: boolean; text: string };

// The API version this app speaks; a PC on an older one may lack features.
const apiVersion = 1;

export async function diagnose(client: AgentClient): Promise<Check[]> {
  let info;
  try {
    info = await client.info();
  } catch (e) {
    if (e instanceof NetworkError) return [{ ok: false, text: t('diag.unreachable') }];
    if (e instanceof AgentError && (e.code === 'unauthorized' || e.code === 'wrong_account')) {
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
    out.push(
      a.available
        ? { ok: true, text: t('diag.claude', { version: a.version ?? '?' }) }
        : { ok: false, text: t('error.agent_unavailable') },
    );
    for (const n of a.notices ?? []) {
      const key = `notice.${n}`;
      out.push({ ok: false, text: has(key) ? t(key) : n });
    }
  }
  return out;
}
