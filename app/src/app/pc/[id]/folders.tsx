// Folders (F10; S5c restyle): the PC's allowed folders, and for each one
// whether it is kept open so the Claude app can start sessions there by
// itself; then the folders with recent conversations, which are saved to
// the allowed list when kept open.

import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';

import type { AgentClient } from '@/api/client';
import type { Folder } from '@/api/types';
import { connect } from '@/connections/open';
import { ConnectionStore } from '@/connections/store';
import { errorText, has, t } from '@/i18n';
import { SwitchRow } from '@/ui/controls';
import { isHistory } from '@/ui/FolderPicker';
import { TopBar } from '@/ui/Header';
import { useSafeInsets } from '@/ui/keyboard';
import { Group, SectionLabel } from '@/ui/List';
import { themed, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

/** unsettled is true while a folder's server has not caught up with its setting. */
function unsettled(f: Folder): boolean {
  const st = f.status?.state ?? 'off';
  return st === 'starting' || (f.server.enabled ? st === 'off' : st === 'running');
}

// While a server is starting, ask again this often, for at most this long.
const followEveryMs = 2000;
const followForMs = 60000;

export default function Folders() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [client, setClient] = useState<AgentClient | null>(null);
  const [folders, setFolders] = useState<Folder[] | null>(null);
  const [supported, setSupported] = useState(true);
  const [error, setError] = useState('');
  const [rowError, setRowError] = useState<Record<string, string>>({});
  const [refreshing, setRefreshing] = useState(false);
  // Folders turned off on this screen: Claude still lists them (R-15).
  const [justOff, setJustOff] = useState<Record<string, boolean>>({});
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

  // After a switch, follow until each server matches its setting: the
  // PC applies the setting a moment later, so the first answer can still
  // say "off" for a folder just turned on (found by E2E-05).
  useEffect(() => {
    if (!client || !folders?.some(unsettled)) return;
    if (Date.now() > followUntil.current) return;
    const timer = setTimeout(() => load(client), followEveryMs);
    return () => clearTimeout(timer);
  }, [client, folders, load]);

  const toggle = async (f: Folder, enabled: boolean) => {
    if (!client) return;
    setRowError((m) => ({ ...m, [f.id]: '' }));
    setJustOff((m) => ({ ...m, [f.id]: !enabled }));
    setFolders((list) => list?.map((x) => (x.id === f.id ? { ...x, server: { ...x.server, enabled } } : x)) ?? list);
    try {
      const saved = await client.setFolderServer(f.id, enabled);
      followUntil.current = Date.now() + followForMs;
      if (isHistory(f)) {
        // It moved to the allowed list under a new id.
        await load(client);
      } else {
        setFolders((list) => list?.map((x) => (x.id === f.id ? saved : x)) ?? list);
      }
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

  const allowed = folders?.filter((f) => !isHistory(f)) ?? [];
  const recent = folders?.filter(isHistory) ?? [];

  const item = (f: Folder) => {
    const st = f.status?.state ?? 'off';
    const stateKey = `folders.state.${st}`;
    const bad = st === 'failed' || st === 'untrusted';
    return (
      <View key={f.id}>
        <SwitchRow
          label={t('folders.keepOpen', { name: f.name || f.id })}
          value={f.server.enabled}
          onChange={(v) => toggle(f, v)}
          disabled={!supported || st === 'external'}
        />
        <View style={s.lines}>
          <Text style={s.path} numberOfLines={2}>
            {f.path}
          </Text>
          <Text style={[s.state, bad && s.error]}>
            {has(stateKey) ? t(stateKey) : st}
            {st === 'running'
              ? ` · ${t('folders.sessions', { n: f.status?.sessions ?? 0, max: f.status?.capacity ?? f.server.capacity ?? 0 })}`
              : ''}
          </Text>
          {justOff[f.id] && !f.server.enabled ? <Text style={s.state}>{t('folders.justOff')}</Text> : null}
          {rowError[f.id] ? <Text style={[s.state, s.error]}>{rowError[f.id]}</Text> : null}
        </View>
      </View>
    );
  };

  return (
    <View style={s.screen}>
      <TopBar title={t('folders.title')} onBack={() => router.back()} />
      <ScrollView
        style={s.screen}
        contentContainerStyle={[s.body, { paddingBottom: 24 + insets.bottom }]}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} colors={[th.mark]} tintColor={th.mark} progressBackgroundColor={th.surface} />}
      >
        <Text style={s.intro}>{t('folders.intro')}</Text>
        {supported ? <Text style={s.note}>{t('folders.offNote')}</Text> : <Text style={s.note}>{t('folders.notSupported')}</Text>}
        {error ? <Text style={[s.note, s.error]}>{error}</Text> : null}
        {folders?.length === 0 ? <Text style={s.note}>{t('new.noFolders')}</Text> : null}
        {allowed.length ? <Group>{allowed.map(item)}</Group> : null}
        {recent.length ? (
          <View style={s.section}>
            <SectionLabel text={t('new.recentFolders')} />
            <Text style={s.hint}>{t('folders.recentHint')}</Text>
            <Group>{recent.map(item)}</Group>
          </View>
        ) : null}
      </ScrollView>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { gap: 14, paddingTop: 4, paddingHorizontal: 16 },
    intro: { paddingHorizontal: 4, fontSize: 15, lineHeight: 22.5, color: th.text },
    note: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
    hint: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
    section: { gap: 6, marginTop: 4 },
    lines: { gap: 2, paddingLeft: 16, paddingRight: 72, paddingBottom: 12, marginTop: -6 },
    path: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    state: { fontSize: 13, lineHeight: 18.2, color: th.textSub },
    error: { color: th.danger },
  }),
);
