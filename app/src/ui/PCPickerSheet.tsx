// The paired PCs with their live state (S5c): the 내 PC screen's PC picker
// (a sheet opened by the switch button next to the PC's name, F-PC) and the
// rows of Settings' paired PCs (F-Settings). Each PC is asked GET /v1/info
// while its row shows, with its own token, so two PCs never mix (F7).

import { useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { AgentClient, baseUrl, CancelledError } from '@/api/client';
import { osName } from '@/connections/label';
import { type Cause, cause } from '@/connections/reach';
import { type Connection, ConnectionStore } from '@/connections/store';
import { t } from '@/i18n';

import { StatusDot, type StatusKind } from './controls';
import { Icon } from './Icon';
import { reachTitle } from './ReachError';
import { rememberOS } from './shell/pcs';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export type Reach = { state: 'checking' } | { state: 'ok'; version: string } | { state: 'error'; cause: Cause };

const checking: Reach = { state: 'checking' };

/**
 * useReach asks each PC of list whether it answers, again whenever the list
 * or `again` changes; null asks nothing (a closed sheet).
 */
export function useReach(list: Connection[] | null, again = 0): Record<string, Reach> {
  const [reach, setReach] = useState<Record<string, Reach>>({});
  const ids = list?.map((c) => c.id).join('|') ?? null;
  useEffect(() => {
    if (!list) return;
    const ctl = new AbortController();
    const store = new ConnectionStore();
    setReach(Object.fromEntries(list.map((c) => [c.id, checking])));
    for (const c of list) {
      (async () => {
        const token = await store.token(c.id);
        const client = new AgentClient(baseUrl(c.host, c.port), { token: token ?? undefined });
        let r: Reach;
        try {
          const info = await client.info(ctl.signal);
          r = { state: 'ok', version: info.version };
          rememberOS(c.id, info.os).catch(() => undefined);
        } catch (e) {
          if (e instanceof CancelledError || ctl.signal.aborted) return;
          r = { state: 'error', cause: cause(e) };
        }
        if (!ctl.signal.aborted) setReach((m) => ({ ...m, [c.id]: r }));
      })();
    }
    return () => ctl.abort();
    // The ids stand for the list: a new array with the same PCs asks nothing again.
  }, [ids, again]);
  return reach;
}

/** reachView is a PC's state as a dot and words; long gives the agent's version or the cause. */
export function reachView(r: Reach | undefined, long: boolean): { kind: StatusKind | 'checking'; text: string; danger: boolean } {
  const x = r ?? checking;
  if (x.state === 'ok') return { kind: 'ok', text: long ? t('home.online', { version: x.version }) : t('pc.connected'), danger: false };
  if (x.state === 'error') return { kind: 'failed', text: long ? reachTitle(x.cause) : t('pc.notConnected'), danger: long };
  return { kind: 'checking', text: t('home.checking'), danger: false };
}

/** PCTile is the rounded square with a PC in it beside a PC's name. */
export function PCTile({ size, tone = 'surface' }: { size: 32 | 40 | 44; tone?: 'surface' | 'raised' }) {
  const th = useTheme();
  return (
    <View
      importantForAccessibility="no-hide-descendants"
      style={{
        width: size,
        height: size,
        borderRadius: size === 32 ? 9 : 12,
        backgroundColor: tone === 'raised' ? th.raised : th.surface,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Icon name="pc" size={size === 32 ? 18 : 22} color={th.textSub} />
    </View>
  );
}

/** ReachLine is a dot and the words of a PC's state. */
export function ReachLine({ reach, long, size = 8 }: { reach: Reach | undefined; long: boolean; size?: number }) {
  const th = useTheme();
  const s = styles(th);
  const v = reachView(reach, long);
  return (
    <View style={s.line}>
      {v.kind === 'checking' ? <StatusDot kind="stopped" size={size} /> : <StatusDot kind={v.kind} size={size} />}
      <Text style={[s.lineText, v.danger && { color: th.danger }]} numberOfLines={2}>
        {v.text}
      </Text>
    </View>
  );
}

/**
 * PCRow is one PC: tile, name and state; selected marks the current one. A
 * long row (the PC picker) also shows the PC's address: the name comes from
 * the PC itself, so two PCs can carry the same one, and the address is what
 * the pairing screen showed (R-94). `detail` is what else tells the PC apart
 * while several are paired (#53): in a long row the system, put before the
 * address; in a short one pcDetail (the system, and the first part of the
 * address when another PC has the same name).
 */
export function PCRow({
  conn,
  reach,
  long,
  selected,
  onPress,
  tile = 40,
  divided,
  detail,
}: {
  conn: Connection;
  reach: Reach | undefined;
  long: boolean;
  selected?: boolean;
  onPress: () => void;
  tile?: 32 | 40;
  divided?: boolean;
  detail?: string;
}) {
  const th = useTheme();
  const s = styles(th);
  const v = reachView(reach, long);
  const apart = long ? [detail, conn.host].filter(Boolean).join(' · ') : (detail ?? '');
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={apart ? `${conn.name}, ${apart}, ${v.text}` : `${conn.name}, ${v.text}`}
      accessibilityState={{ selected: !!selected }}
      style={({ pressed }) => [s.row, tile === 32 ? s.rowSmall : s.rowBig, divided && s.divided, pressed && s.pressed]}
    >
      <PCTile size={tile} tone="raised" />
      <View style={s.names}>
        <Text style={s.name} numberOfLines={1}>
          {conn.name}
        </Text>
        {apart ? (
          <Text style={s.host} numberOfLines={1} ellipsizeMode="middle">
            {apart}
          </Text>
        ) : null}
        <ReachLine reach={reach} long={long} size={tile === 32 ? 7 : 8} />
      </View>
      {selected ? <Icon name="check" size={18} color={th.mark} /> : <Icon name="chevronRight" size={18} color={th.caption} />}
    </Pressable>
  );
}

/** PCPickerSheet lists the paired PCs; picking one makes it the current PC. */
export function PCPickerSheet({
  open,
  onClose,
  list,
  currentId,
  onPick,
  onPair,
}: {
  open: boolean;
  onClose: () => void;
  list: Connection[];
  currentId: string | null;
  onPick: (id: string) => void;
  onPair: () => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const reach = useReach(open ? list : null);
  return (
    <Sheet open={open} onClose={onClose} title={t('pc.pickTitle')} testID="pc-picker">
      <View style={s.list}>
        {list.map((c, i) => (
          <PCRow
            key={c.id}
            conn={c}
            reach={reach[c.id]}
            long
            detail={list.length > 1 ? osName(c.os) : ''}
            selected={c.id === currentId}
            onPress={() => onPick(c.id)}
            divided={i > 0}
          />
        ))}
        <Pressable
          onPress={onPair}
          accessibilityRole="button"
          accessibilityLabel={t('home.pair')}
          style={({ pressed }) => [s.row, s.rowPair, list.length > 0 && s.divided, pressed && s.pressed]}
        >
          <View style={s.plus}>
            <Icon name="plus" size={20} color={th.accent} />
          </View>
          <Text style={s.pair}>{t('home.pair')}</Text>
        </Pressable>
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    list: { paddingTop: 4 },
    row: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 20, paddingRight: 14 },
    rowBig: { minHeight: 64, paddingVertical: 10 },
    rowSmall: { minHeight: 60, paddingLeft: 16, paddingRight: 12 },
    rowPair: { minHeight: 56 },
    divided: { borderTopWidth: 1, borderTopColor: th.divider },
    pressed: { backgroundColor: th.raised },
    names: { flex: 1, minWidth: 0, gap: 2 },
    name: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    host: { fontSize: 12, lineHeight: 17, color: th.caption },
    line: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    lineText: { flexShrink: 1, fontSize: 13, lineHeight: 18, color: th.textSub },
    plus: { width: 40, alignItems: 'center' },
    pair: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.accent },
  }),
);
