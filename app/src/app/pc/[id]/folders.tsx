// Folders (F10): the PC's allowed folders, and for each one whether it is
// kept open so the Claude app can start sessions there by itself.

import { useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Switch, Text, View } from 'react-native';

import type { AgentClient } from '@/api/client';
import type { Folder } from '@/api/types';
import { connect } from '@/connections/open';
import { ConnectionStore } from '@/connections/store';
import { errorText, has, t } from '@/i18n';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

// While a server is starting, ask again this often, for at most this long.
const followEveryMs = 2000;
const followForMs = 60000;

export default function Folders() {
  const th = useTheme();
  const s = styles(th);
  const { id } = useLocalSearchParams<{ id: string }>();
  const [client, setClient] = useState<AgentClient | null>(null);
  const [folders, setFolders] = useState<Folder[] | null>(null);
  const [supported, setSupported] = useState(true);
  const [error, setError] = useState('');
  const [rowError, setRowError] = useState<Record<string, string>>({});
  const [refreshing, setRefreshing] = useState(false);
  const followUntil = useRef(0);

  const load = useCallback(async (c: AgentClient) => {
    try {
      const r = await c.folders();
      setFolders(r.folders);
      setError('');
      return r.folders;
    } catch (e) {
      setError(errorText(e));
      return null;
    }
  }, []);

  useEffect(() => {
    (async () => {
      const pc = await connect(store, id);
      if (!pc) return;
      setClient(pc.client);
      pc.client
        .info()
        .then((i) => setSupported(i.capabilities.includes('serve')))
        .catch(() => undefined);
      load(pc.client);
    })();
  }, [id, load]);

  // Follow a server that is starting until it settles.
  useEffect(() => {
    if (!client || !folders?.some((f) => f.status?.state === 'starting')) return;
    if (Date.now() > followUntil.current) return;
    const timer = setTimeout(() => load(client), followEveryMs);
    return () => clearTimeout(timer);
  }, [client, folders, load]);

  const toggle = async (f: Folder, enabled: boolean) => {
    if (!client) return;
    setRowError((m) => ({ ...m, [f.id]: '' }));
    setFolders((list) => list?.map((x) => (x.id === f.id ? { ...x, server: { ...x.server, enabled } } : x)) ?? list);
    try {
      const saved = await client.setFolderServer(f.id, enabled);
      setFolders((list) => list?.map((x) => (x.id === f.id ? saved : x)) ?? list);
      followUntil.current = Date.now() + followForMs;
    } catch (e) {
      setRowError((m) => ({ ...m, [f.id]: errorText(e) }));
      load(client);
    }
  };

  const refresh = async () => {
    if (!client) return;
    setRefreshing(true);
    await load(client);
    setRefreshing(false);
  };

  return (
    <ScrollView
      style={s.screen}
      contentContainerStyle={s.body}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
    >
      <Text style={s.intro}>{t('folders.intro')}</Text>
      {!supported ? <Text style={s.muted}>{t('folders.notSupported')}</Text> : null}
      {error ? <Text style={s.error}>{error}</Text> : null}
      {folders?.length === 0 ? <Text style={s.muted}>{t('new.noFolders')}</Text> : null}
      {folders?.map((f) => {
        const st = f.status?.state ?? 'off';
        const stateKey = `folders.state.${st}`;
        return (
          <View key={f.id} style={s.card}>
            <View style={s.top}>
              <View style={s.names}>
                <Text style={s.title}>{f.name || f.id}</Text>
                <Text style={s.muted} numberOfLines={2}>
                  {f.path}
                </Text>
              </View>
              <Switch
                value={f.server.enabled}
                onValueChange={(v) => toggle(f, v)}
                disabled={!supported || st === 'external'}
                accessibilityLabel={t('folders.keepOpen', { name: f.name || f.id })}
              />
            </View>
            <Text style={st === 'failed' || st === 'untrusted' ? s.error : s.muted}>
              {has(stateKey) ? t(stateKey) : st}
              {st === 'running'
                ? ` · ${t('folders.sessions', { n: f.status?.sessions ?? 0, max: f.status?.capacity ?? f.server.capacity ?? 0 })}`
                : ''}
            </Text>
            {rowError[f.id] ? <Text style={s.error}>{rowError[f.id]}</Text> : null}
          </View>
        );
      })}
    </ScrollView>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { padding: 16, gap: 12 },
    intro: { color: th.text, fontSize: 15, lineHeight: 22 },
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 14,
      gap: 6,
    },
    top: { flexDirection: 'row', alignItems: 'center', gap: 12 },
    names: { flex: 1, gap: 2 },
    title: { color: th.text, fontSize: 16, fontWeight: '600' },
    muted: { color: th.muted, fontSize: 13 },
    error: { color: th.danger, fontSize: 14, lineHeight: 20 },
  });
