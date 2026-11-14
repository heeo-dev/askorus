// A paired phone answers a pairing request when nobody is at the PC (#58).
// While the app is open it asks the current PC now and then whether another
// phone of the same Tailscale account is waiting to be let in, and shows the
// question: the phone's name and the two digits it shows. The person allows
// it only when they asked for it themselves and the number is the same.

import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, StyleSheet, Text, View } from 'react-native';

import { type AgentClient, AgentError } from '@/api/client';
import type { PairQuestion } from '@/api/types';
import { t } from '@/i18n';

import { Button } from './Button';
import { Sheet } from './Sheet';
import { currentPC } from './shell/pcs';
import { themed, useTheme } from './theme';

/** How often the current PC is asked (tests shorten it). */
export const pairAsk = { everyMs: 5000 };

type Asked = { pcId: string; pcName: string; client: AgentClient; q: PairQuestion };

// Connections that are not asked again: the agent has no such list (older
// than #58), or it refused this phone's token (the phone was disconnected on
// the PC). Asking on with a dead token every few seconds fills the agent's
// limit of failed attempts within a minute, and the phone then cannot even
// pair again for ten minutes (found on a real phone, R-115). The key holds
// the device id, so the same PC paired again is asked again.
const without = new Set<string>();

/**
 * The request this phone itself is making right now (the pairing screen
 * sets it while it waits): a phone that is paired already and pairs again
 * is not "another phone" to ask itself about.
 */
export const ownAsk = { name: '', match: '' };

export function PairRequests() {
  const s = styles(useTheme());
  const [asked, setAsked] = useState<Asked | null>(null);
  const [busy, setBusy] = useState(false);
  const seen = useRef(new Set<string>()); // requests this phone has answered or closed
  const shown = useRef<Asked | null>(null); // what the sheet shows now

  const close = useCallback(() => {
    if (shown.current) seen.current.add(shown.current.q.id);
    shown.current = null;
    setAsked(null);
    setBusy(false);
  }, []);

  useEffect(() => {
    let live = true;
    const look = async () => {
      if (!live || AppState.currentState === 'background') return;
      let pcId = '';
      let key = '';
      try {
        const cur = await currentPC();
        if (!live || !cur) return;
        key = `${cur.conn.id}:${cur.conn.deviceId}`;
        if (without.has(key)) return;
        pcId = cur.conn.id;
        const list = await cur.client.pairRequests();
        if (!live) return;
        const now = shown.current;
        if (now) {
          // Answered elsewhere (the PC's screen, a pairing window, another
          // phone) or its time is up: the question goes away by itself (found
          // on a real phone: it stayed, with buttons that did nothing).
          if (now.pcId === pcId && !list.some((x) => x.id === now.q.id)) close();
          return;
        }
        const q = list.find((x) => !seen.current.has(x.id) && !(x.match === ownAsk.match && x.deviceName === ownAsk.name));
        if (q) {
          shown.current = { pcId, pcName: cur.conn.name, client: cur.client, q };
          setAsked(shown.current);
        }
      } catch (e) {
        // An agent from before #58 has no list, and a refused token stays
        // refused. Anything else (not signed in, the PC is off, too many
        // attempts) is asked again later.
        if (key && e instanceof AgentError && [401, 404, 405].includes(e.status)) without.add(key);
      }
    };
    const timer = setInterval(look, pairAsk.everyMs);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [close]);

  const answer = async (allow: boolean) => {
    if (!asked) return;
    setBusy(true);
    try {
      await asked.client.answerPair(asked.q.id, allow);
    } catch {
      // Answered elsewhere meanwhile, or its time is up: nothing more to do here.
    }
    close();
  };

  return (
    <Sheet open={!!asked} onClose={close} title={t('pairAsk.title')} testID="pair-ask">
      {asked ? (
        <View style={s.body}>
          <Text style={s.text}>{t('pairAsk.body', { pc: asked.pcName })}</Text>
          <View style={s.card}>
            <Text style={s.name}>{asked.q.deviceName}</Text>
            <Text style={s.match} accessibilityLabel={t('pairAsk.number', { n: asked.q.match })}>
              {asked.q.match}
            </Text>
          </View>
          <Text style={s.warn}>{t('pairAsk.warn')}</Text>
          <Button label={t('pairAsk.allow')} onPress={() => answer(true)} disabled={busy} />
          <Button variant="outline" label={t('pairAsk.deny')} onPress={() => answer(false)} disabled={busy} />
        </View>
      ) : null}
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 14, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    text: { fontSize: 15, lineHeight: 21.75, color: th.text },
    card: { alignItems: 'center', gap: 6, padding: 18, borderRadius: 14, backgroundColor: th.surface },
    name: { fontSize: 17, fontWeight: '600', lineHeight: 24, color: th.text, textAlign: 'center' },
    match: { fontSize: 40, fontWeight: '700', lineHeight: 48, letterSpacing: 6, color: th.text },
    warn: { fontSize: 14, lineHeight: 20.3, color: th.textSub },
  }),
);
