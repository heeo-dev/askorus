// The agent update from the phone (#35): the PC's version and the latest
// release, the work an update cuts off, the go, and what came of it. It is
// a panel, not a sheet, so Settings' PC sheet holds it in place and the
// old-agent card puts it in a sheet of its own.

import { useCallback, useEffect, useRef, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { AgentError, type AgentClient } from '@/api/client';
import { followUpdate, type UpdateOutcome, type UpdatePhase } from '@/api/selfUpdate';
import type { UpdateStatus } from '@/api/types';
import { errorText, has, t } from '@/i18n';

import { Button } from './Button';
import { Spinner } from './controls';
import { themed, useTheme } from './theme';

type State =
  | { kind: 'loading' }
  | { kind: 'error'; text: string }
  | { kind: 'shown'; st: UpdateStatus; note?: string }
  | { kind: 'running'; to: string; phase: UpdatePhase | 'starting' }
  | { kind: 'ended'; to: string; outcome: UpdateOutcome };

/** whyText is why this agent cannot update itself, in words. */
function whyText(why: string | undefined, deviceId: string): string {
  const key = `update.why.${why ?? ''}`;
  return has(key) ? t(key, { id: deviceId }) : t('update.why.other');
}

export function UpdatePanel({
  client,
  deviceId,
  onEnded,
}: {
  client: AgentClient | null;
  /** This phone's id on the PC, for the command that allows it. */
  deviceId: string;
  /** Told once an update has ended, whatever came of it (the PC may now be another version). */
  onEnded?: (o: UpdateOutcome) => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const [state, setState] = useState<State>({ kind: 'loading' });
  const following = useRef<AbortController | null>(null);

  const load = useCallback(
    async (note?: string) => {
      if (!client) return;
      setState({ kind: 'loading' });
      try {
        setState({ kind: 'shown', st: await client.updateStatus(), note });
      } catch (e) {
        setState({ kind: 'error', text: errorText(e) });
      }
    },
    [client],
  );

  useEffect(() => {
    load();
    return () => following.current?.abort();
  }, [load]);

  const go = async (st: UpdateStatus) => {
    if (!client || !st.latest) return;
    const to = st.latest;
    setState({ kind: 'running', to, phase: 'starting' });
    try {
      await client.startUpdate(to);
    } catch (e) {
      // The release changed meanwhile: show the one there is now.
      if (e instanceof AgentError && e.code === 'update_changed') return load(t('update.changed'));
      const key = e instanceof AgentError ? `update.error.${e.code}` : '';
      setState({ kind: 'shown', st, note: key && has(key) ? t(key, { id: deviceId }) : errorText(e) });
      return;
    }
    const ac = new AbortController();
    following.current = ac;
    const outcome = await followUpdate(client, to, { signal: ac.signal, onPhase: (phase) => setState({ kind: 'running', to, phase }) });
    if (ac.signal.aborted) return;
    setState({ kind: 'ended', to, outcome });
    onEnded?.(outcome);
  };

  if (!client) return null;
  if (state.kind === 'loading') {
    return (
      <View style={s.row} testID="update-panel">
        <Spinner size={18} />
        <Text style={s.caption}>{t('update.checking')}</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={s.panel} testID="update-panel">
        <Text style={s.bad}>{state.text}</Text>
        <Button variant="quiet" icon="retry" label={t('update.again')} onPress={() => load()} />
      </View>
    );
  }
  if (state.kind === 'running') {
    const words = state.phase === 'waiting' ? t('update.waiting') : state.phase === 'away' ? t('update.away') : t('update.starting', { version: state.to });
    return (
      <View style={s.row} testID="update-panel" accessibilityLiveRegion="polite">
        <Spinner size={18} />
        <Text style={s.body}>{words}</Text>
      </View>
    );
  }
  if (state.kind === 'ended') {
    const o = state.outcome;
    const words =
      o.kind === 'done' ? t('update.done', { version: o.version }) : o.kind === 'rolledBack' ? t('update.rolledBack') : o.kind === 'failed' ? t('update.failed') : o.kind === 'notTaken' ? t('update.notTaken') : t('update.lost');
    return (
      <View style={s.panel} testID="update-panel" accessibilityLiveRegion="polite">
        <Text style={o.kind === 'done' ? s.good : s.bad}>{words}</Text>
        {o.kind !== 'done' && o.kind !== 'lost' && o.reason ? <Text style={s.caption}>{o.reason}</Text> : null}
        {o.kind !== 'done' ? <Button variant="quiet" icon="retry" label={t('update.again')} onPress={() => load()} /> : null}
      </View>
    );
  }

  const st = state.st;
  const lines: string[] = [];
  let canGo = false;
  if (!st.on) {
    lines.push(whyText('off', deviceId));
  } else if (st.checkError) {
    const key = `update.check.${st.checkError}`;
    lines.push(has(key) ? t(key) : t('update.check.unreachable'));
  } else if (!st.newer || !st.latest) {
    lines.push(t('update.upToDate', { current: st.current }));
  } else {
    lines.push(t('update.newer', { current: st.current, latest: st.latest }));
    if (!st.can) lines.push(whyText(st.why, deviceId));
    else if (!st.allowed) lines.push(t('update.notAllowed', { id: deviceId }));
    else if (st.running) lines.push(t('update.error.update_running'));
    else {
      canGo = true;
      if (st.busy.asks > 0) lines.push(t('update.cutAsks', { n: st.busy.asks }));
      if (st.busy.folders > 0) lines.push(t('update.cutFolders', { n: st.busy.folders }));
      lines.push(t('update.cutPhone'));
    }
  }
  return (
    <View style={s.panel} testID="update-panel">
      {state.note ? <Text style={s.bad}>{state.note}</Text> : null}
      {lines.map((l, i) => (
        <Text key={i} style={i === 0 ? s.body : s.caption}>
          {l}
        </Text>
      ))}
      {canGo ? <Button label={t('update.go')} onPress={() => go(st)} /> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    panel: { gap: 8 },
    row: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 40 },
    body: { flexShrink: 1, fontSize: 15, lineHeight: 22, color: th.text },
    caption: { flexShrink: 1, fontSize: 13, lineHeight: 19.5, color: th.caption },
    good: { fontSize: 15, lineHeight: 22, color: th.ok },
    bad: { fontSize: 15, lineHeight: 22, color: th.danger },
  }),
);
