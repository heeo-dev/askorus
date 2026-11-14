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

type Asked = { pcName: string; client: AgentClient; q: PairQuestion };

// PCs whose agent has no such list (older than #58): not asked again.
const without = new Set<string>();

export function PairRequests() {
  const s = styles(useTheme());
  const [asked, setAsked] = useState<Asked | null>(null);
  const [busy, setBusy] = useState(false);
  const seen = useRef(new Set<string>()); // requests this phone has answered or closed
  const open = useRef(false);

  useEffect(() => {
    let live = true;
    const look = async () => {
      if (!live || open.current || AppState.currentState === 'background') return;
      let pcId = '';
      try {
        const cur = await currentPC();
        if (!live || !cur || without.has(cur.conn.id)) return;
        pcId = cur.conn.id;
        const list = await cur.client.pairRequests();
        const q = list.find((x) => !seen.current.has(x.id));
        if (live && q && !open.current) {
          open.current = true;
          setAsked({ pcName: cur.conn.name, client: cur.client, q });
        }
      } catch (e) {
        // An agent from before #58 has no list. Anything else (not signed in,
        // the PC is off) is asked again later.
        if (pcId && e instanceof AgentError && (e.status === 404 || e.status === 405)) without.add(pcId);
      }
    };
    const timer = setInterval(look, pairAsk.everyMs);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  const close = useCallback(() => {
    open.current = false;
    setAsked((a) => {
      if (a) seen.current.add(a.q.id);
      return null;
    });
    setBusy(false);
  }, []);

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
