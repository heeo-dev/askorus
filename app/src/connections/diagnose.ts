// Connection checks for one PC, in the order things break: can the phone
// reach the PC, does the PC still know this phone, is Claude Code there.

import { agentBehind, appVersion } from '../api/agentUpdate';
import { agentName, noticeIsNote, noticeText, signInNotice } from '../api/agents';
import type { AgentClient } from '../api/client';
import { errorText, type MessageKey, t } from '../i18n';
import { cause, causesToCheck } from './reach';

const checkKey: Record<string, MessageKey> = {
  phone_offline: 'reach.check.phone',
  pc_unreachable: 'reach.check.pc',
  not_own_pc: 'reach.check.not_own',
};

/**
 * note: nothing is wrong, but something is missing (an optional AI).
 * ai: the line is about installing or signing in to an AI, so the sheet
 * offers the README's guide (#60).
 */
export type Check = { ok: boolean; text: string; note?: boolean; ai?: boolean };

// Claude Code runs the remote sessions; the other AIs only answer
// questions for now (Phase 2), so a missing one is a note, not a failure.

// The API version this app speaks; a PC on an older one may lack features.
const apiVersion = 1;

export async function diagnose(client: AgentClient): Promise<Check[]> {
  let info;
  try {
    info = await client.info();
  } catch (e) {
    const c = cause(e);
    if (c === 'phone_offline' || c === 'pc_unreachable' || c === 'not_own_pc') {
      return [
        { ok: false, text: t(c === 'not_own_pc' ? 'reach.not_own_pc' : 'reach.offline') },
        ...causesToCheck(c).map((x) => ({ ok: false, text: t(checkKey[x]) })),
      ];
    }
    if (c === 'tailnet_login') return [{ ok: false, text: errorText(e) }];
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
  // Older than this app (11-18): it still works, but the new features wait for it.
  const app = appVersion();
  if (agentBehind(info.version, app)) out.push({ ok: true, note: true, text: t('diag.agentBehind', { want: app }) });
  for (const a of info.agents) {
    const name = agentName(a.agent);
    if (a.agent === 'claude') {
      out.push(
        a.available
          ? { ok: true, text: t('diag.claude', { version: a.version ?? '?' }) }
          : { ok: false, ai: true, text: t('notice.claude_not_found') },
      );
    } else if (a.available) {
      out.push({ ok: true, text: t('diag.askAgent', { name, version: a.version ?? '?' }) });
    } else {
      // Not installed, or there but not working (only an npm script, or
      // its version check failed): a note either way, with the reason.
      const notices = a.notices ?? [];
      const missing = notices.every((n) => n === `${a.agent}_not_found`);
      out.push({ ok: true, note: true, ai: true, text: t(missing ? 'diag.askAgentMissing' : 'diag.askAgentBroken', { name }) });
      for (const n of missing ? [] : notices) {
        out.push({ ok: true, note: true, ai: true, text: noticeText(n, a) });
      }
      continue;
    }
    // A signed-out AI is a failure; a sign-in that ends soon is a note (#60).
    for (const n of a.notices ?? []) {
      const ai = signInNotice(n);
      out.push(noticeIsNote(n) ? { ok: true, note: true, ai, text: noticeText(n, a) } : { ok: false, ai, text: noticeText(n, a) });
    }
  }
  return out;
}
