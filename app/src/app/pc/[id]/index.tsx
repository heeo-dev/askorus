// One PC's sessions (F1, F2): search, state filter, badges, pull to
// refresh. Tapping a session opens its sheet.

import { router, Stack, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AppState,
  FlatList,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import type { AgentClient } from '@/api/client';
import type { Session } from '@/api/types';
import { connect } from '@/connections/open';
import { type Connection, ConnectionStore } from '@/connections/store';
import { type Cause, cause } from '@/connections/reach';
import { errorText, has, label, type MessageKey, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { ReachError } from '@/ui/ReachError';
import { SessionSheet } from '@/ui/SessionSheet';
import { ago, folderName, sessionTitle } from '@/ui/format';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

const filters = ['', 'running', 'stopped', 'terminal', 'history'] as const;
type Filter = (typeof filters)[number];

type Load = { state: 'loading' } | { state: 'ok' } | { state: 'error'; cause: Cause; text: string };

export default function PCSessions() {
  const th = useTheme();
  const s = styles(th);
  const { id } = useLocalSearchParams<{ id: string }>();
  const [pc, setPC] = useState<{ conn: Connection; client: AgentClient } | null | undefined>(undefined);
  const [caps, setCaps] = useState<string[]>([]);
  const [notices, setNotices] = useState<string[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [refreshing, setRefreshing] = useState(false);
  const [query, setQuery] = useState('');
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState<Filter>('');
  const [selected, setSelected] = useState<Session | null>(null);
  const seq = useRef(0);

  useEffect(() => {
    connect(store, id).then(setPC);
  }, [id]);

  useEffect(() => {
    if (!pc) return;
    pc.client
      .info()
      .then((i) => {
        setCaps(i.capabilities);
        // Only the AIs whose sessions this list shows; one that only
        // answers questions is reported in the connection check.
        setNotices(i.agents.filter((a) => a.capabilities.includes('list')).flatMap((a) => a.notices ?? []));
      })
      .catch(() => setCaps([]));
  }, [pc]);

  // Typing settles for a moment before the PC is asked.
  useEffect(() => {
    const timer = setTimeout(() => setQ(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  const fetchSessions = useCallback(async () => {
    if (!pc) return;
    const mine = ++seq.current;
    try {
      const r = await pc.client.sessions({ q, state: filter || undefined });
      if (mine !== seq.current) return; // a newer search answered first
      setSessions(r.sessions);
      setWarnings(r.warnings ?? []);
      setLoad({ state: 'ok' });
    } catch (e) {
      if (mine !== seq.current) return;
      setLoad({ state: 'error', cause: cause(e), text: errorText(e) });
    }
  }, [pc, q, filter]);

  // Also on coming back from a new session or the folders screen.
  useFocusEffect(
    useCallback(() => {
      fetchSessions();
    }, [fetchSessions]),
  );

  // Coming back from the Claude app: states have likely changed.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (st) => {
      if (st === 'active') fetchSessions();
    });
    return () => sub.remove();
  }, [fetchSessions]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await fetchSessions();
    } finally {
      setRefreshing(false);
    }
  };

  const replace = (next: Session) => {
    setSessions((list) => list.map((x) => (x.id === next.id ? next : x)));
    setSelected((cur) => (cur && cur.id === next.id ? next : cur));
  };

  if (pc === null) {
    return (
      <View style={[s.screen, s.center]}>
        <Text style={s.muted}>{t('sessions.gone')}</Text>
      </View>
    );
  }

  return (
    <View style={s.screen}>
      <Stack.Screen options={{ title: pc?.conn.name ?? '' }} />
      <View style={s.top}>
        <View style={s.actions}>
          <View style={s.action}>
            <Button label={t('sessions.new')} onPress={() => router.push({ pathname: '/pc/[id]/new', params: { id } })} />
          </View>
          <View style={s.action}>
            <Button
              secondary
              label={t('sessions.folders')}
              onPress={() => router.push({ pathname: '/pc/[id]/folders', params: { id } })}
            />
          </View>
        </View>
        <TextInput
          style={s.search}
          value={query}
          onChangeText={setQuery}
          placeholder={t('sessions.search')}
          placeholderTextColor={th.muted}
          autoCapitalize="none"
          autoCorrect={false}
          clearButtonMode="while-editing"
          returnKeyType="search"
        />
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.filters}>
          {filters.map((f) => (
            <Pressable
              key={f || 'all'}
              onPress={() => setFilter(f)}
              style={[s.filter, filter === f && s.filterOn]}
              accessibilityRole="button"
              accessibilityState={{ selected: filter === f }}
            >
              <Text style={[s.filterText, filter === f && s.filterTextOn]}>
                {f ? label('state', f) : t('sessions.all')}
              </Text>
            </Pressable>
          ))}
        </ScrollView>
      </View>

      {warnings.includes('claude_agents_unavailable') ? (
        <Text style={s.warning}>{t('sessions.agentsUnavailable')}</Text>
      ) : null}

      {notices.map((n) => (
        <Text key={n} style={s.notice}>
          {has(`notice.${n}`) ? t(`notice.${n}` as MessageKey) : n}
        </Text>
      ))}

      {load.state === 'error' ? (
        <View style={s.errorBox}>
          <ReachError cause={load.cause} detail={load.text} onRetry={refresh} />
        </View>
      ) : null}

      <FlatList
        data={sessions}
        keyExtractor={(x) => x.id}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
        contentContainerStyle={s.list}
        keyboardShouldPersistTaps="handled"
        ListEmptyComponent={
          load.state === 'ok' ? (
            <Text style={[s.muted, s.empty]}>
              {q || filter ? t('sessions.emptyFiltered') : t('sessions.empty')}
            </Text>
          ) : undefined
        }
        renderItem={({ item }) => <SessionRow th={th} session={item} onPress={() => setSelected(item)} />}
      />

      {pc ? (
        <SessionSheet
          session={selected}
          client={pc.client}
          capabilities={caps}
          onClose={() => setSelected(null)}
          onChanged={replace}
          onRemoved={(gone) => {
            setSelected(null);
            setSessions((list) => list.filter((x) => x.id !== gone));
          }}
        />
      ) : null}
    </View>
  );
}

function SessionRow({ th, session, onPress }: { th: Theme; session: Session; onPress: () => void }) {
  const s = styles(th);
  const running = session.state === 'running';
  const meta = [folderName(session.folder), ago(session.lastActive)].filter(Boolean).join(' · ');
  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [s.row, pressed && s.pressed]}
      accessibilityRole="button"
    >
      <View style={s.rowTop}>
        <Text style={s.rowTitle} numberOfLines={1}>
          {sessionTitle(session)}
        </Text>
        <Text style={[s.state, running && s.stateRunning]}>{label('state', session.state)}</Text>
      </View>
      <Text style={s.muted} numberOfLines={1}>
        {meta}
      </Text>
      {session.badges?.length ? (
        <View style={s.badges}>
          {session.badges.map((b) => (
            <Text key={b} style={s.badge}>
              {label('badge', b)}
            </Text>
          ))}
        </View>
      ) : null}
    </Pressable>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    center: { alignItems: 'center', justifyContent: 'center', padding: 24 },
    top: { paddingHorizontal: 16, paddingTop: 12, gap: 10 },
    actions: { flexDirection: 'row', gap: 10 },
    action: { flex: 1 },
    search: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 10,
      color: th.text,
      fontSize: 15,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    filters: { gap: 8, paddingBottom: 4 },
    filter: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 14,
      paddingVertical: 6,
      backgroundColor: th.card,
    },
    filterOn: { backgroundColor: th.accent, borderColor: th.accent },
    filterText: { color: th.text, fontSize: 14 },
    filterTextOn: { color: th.accentText, fontWeight: '600' },
    warning: { color: th.muted, fontSize: 13, paddingHorizontal: 16, paddingTop: 8 },
    errorBox: { paddingHorizontal: 16, paddingTop: 12, gap: 10 },
    notice: { color: th.danger, fontSize: 14, lineHeight: 20, paddingHorizontal: 16, paddingTop: 8 },
    list: { padding: 16, gap: 10 },
    empty: { textAlign: 'center', paddingTop: 32 },
    row: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 14,
      gap: 4,
    },
    pressed: { opacity: 0.7 },
    rowTop: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    rowTitle: { flex: 1, color: th.text, fontSize: 16, fontWeight: '600' },
    state: { color: th.muted, fontSize: 13 },
    stateRunning: { color: th.ok, fontWeight: '600' },
    muted: { color: th.muted, fontSize: 14 },
    badges: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginTop: 2 },
    badge: {
      color: th.accent,
      fontSize: 12,
      borderColor: th.accent,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 8,
      paddingVertical: 2,
      overflow: 'hidden',
    },
  });
