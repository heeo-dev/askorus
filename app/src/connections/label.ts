// How a paired PC is told apart from the others (#53). A PC's name is its
// host name unless the person renamed it, and two PCs can carry the same
// one (a Windows PC and the Linux inside it). So beside the name goes the
// PC's system, and when a name is shared, the first part of its address.

import { cleanDeviceName } from './pair';
import type { Connection } from './store';

const systems: Record<string, string> = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' };

/** osName is a system as people call it; '' for one not known. */
export function osName(os: string | undefined): string {
  return systems[(os ?? '').toLowerCase()] ?? '';
}

const same = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/** sharesName reports whether another PC of list carries c's name. */
export function sharesName(c: Connection, list: readonly Connection[] | null | undefined): boolean {
  return !!list?.some((x) => x.id !== c.id && same(x.name, c.name));
}

/**
 * pcDetail is what goes beside a PC's name while more than one PC is
 * paired: its system, and the first part of its address when another PC
 * has the same name. '' with a single PC (nothing to tell apart) or when
 * there is nothing to add.
 */
export function pcDetail(c: Connection, list: readonly Connection[] | null | undefined): string {
  if ((list?.length ?? 0) < 2) return '';
  const parts = [osName(c.os)];
  if (sharesName(c, list)) parts.push(c.host.split('.')[0]);
  return parts.filter(Boolean).join(' · ');
}

/** pcLabel is a PC in one line of words: its name, with the detail in brackets. */
export function pcLabel(c: Connection, list: readonly Connection[] | null | undefined): string {
  const detail = pcDetail(c, list);
  return detail ? `${c.name} (${detail})` : c.name;
}

/** The longest name a person can give a PC. */
export const maxPCName = 40;

/** cleanPCName is a name a person typed for a PC: printable, trimmed, at most maxPCName; '' when nothing is left. */
export function cleanPCName(name: string): string {
  return Array.from(cleanDeviceName(name, '')).slice(0, maxPCName).join('').trim();
}
