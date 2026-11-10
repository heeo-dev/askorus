// 내 PC / My PC (S5c, board F-PC): the current PC (the one that also answers
// questions) with its state, its sessions (F1, F2: search, state filter,
// pull to refresh; a session waiting for input first in apricot), the
// folders kept open (F10) and the button for a new session (F4). The PC
// picker sheet switches PCs and leads to pairing. Tapping a session opens
// its sheet; so does the drawer's waiting session, through the `session`
// search param.
//
// Each full (unfiltered) list it gets goes to the shell's status
// (publishSessions), so the waiting dot needs no second read.

import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, FlatList, Pressable, RefreshControl, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';

import type { AgentClient } from '@/api/client';
import type { Folder, Session } from '@/api/types';
import { connect } from '@/connections/open';
import { type Cause, cause } from '@/connections/reach';
import { type Connection, ConnectionStore } from '@/connections/store';
import { errorText, has, label, type MessageKey, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { Chip, Spinner, StatusDot } from '@/ui/controls';
import { ago, sessionStatus, sessionTitle } from '@/ui/format';
import { Header } from '@/ui/Header';
import { Icon, IconButton } from '@/ui/Icon';
import { useSafeInsets } from '@/ui/keyboard';
import { SectionLabel } from '@/ui/List';
import { PCPickerSheet, PCTile, type Reach, ReachLine } from '@/ui/PCPickerSheet';
import { ReachError } from '@/ui/ReachError';
import { SessionSheet } from '@/ui/SessionSheet';
import { usePCs } from '@/ui/shell/pcs';
import { publishError, publishSessions } from '@/ui/shell/pcStatus';
import { themed, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

// 'ask' shows only the sessions of asks (badge ask, P0-8); every other
// chip leaves them out, so coding sessions are not buried under asks.
const filters = ['', 'running', 'stopped', 'terminal', 'history', 'ask'] as const;
type Filter = (typeof filters)[number];

const isAsk = (x: Session) => x.badges?.includes('ask') ?? false;

type Load = { state: 'loading' } | { state: 'ok' } | { state: 'error'; cause: Cause; text: string };
type PC = { conn: Connection; client: AgentClient };

export default function MyPC() {
  const th = useTheme();
  const s = styles(th);
  const { list, current, pick, reload } = usePCs();
  const [pickerOpen, setPickerOpen] = useState(false);
  // Pairing a further PC comes back with pick=1: the picker opens with both
  // PCs, and the current one stays until the person picks (R-94).
  const { pick: wantPick } = useLocalSearchParams<{ pick?: string }>();
  useEffect(() => {
    if (wantPick !== '1') return;
    setPickerOpen(true);
    router.setParams({ pick: undefined });
  }, [wantPick]);

  // Coming back from pairing (or Settings' unpair) shows the PCs as they are now.
  useFocusEffect(
    useCallback(() => {
      reload().catch(() => undefined);
    }, [reload]),
  );

  let body;
  if (list === null) {
    body = (
      <>
        <Header entrance="pcs" />
        <View style={s.center}>
          <Spinner size={24} />
        </View>
      </>
    );
  } else if (!current) {
    body = (
      <>
        <Header entrance="pcs" />
        <View style={s.empty}>
          <PCTile size={44} />
          <Text style={s.emptyTitle} accessibilityRole="header">
            {t('home.empty')}
          </Text>
          <Text style={s.emptyHint}>{t('home.emptyHint')}</Text>
          <Button icon="plus" label={t('home.pair')} onPress={() => router.push('/pair')} style={s.emptyButton} />
        </View>
      </>
    );
  } else {
    body = <Sessions key={current.id} conn={current} onPick={() => setPickerOpen(true)} />;
  }

  return (
    <View style={s.screen}>
      {body}
      <PCPickerSheet
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        list={list ?? []}
        currentId={current?.id ?? null}
        onPick={(id) => {
          setPickerOpen(false);
          if (id !== current?.id) pick(id).catch(() => undefined);
        }}
        onPair={() => {
          setPickerOpen(false);
          router.push('/pair');
        }}
      />
    </View>
  );
}

// Sessions is one PC's part of the screen; a new current PC mounts a new one
// (key), so nothing of the last PC stays.
function Sessions({ conn, onPick }: { conn: Connection; onPick: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { session: wantId } = useLocalSearchParams<{ session?: string }>();
  const [pc, setPC] = useState<PC | null | undefined>(undefined);
  const [info, setInfo] = useState<Reach>({ state: 'checking' });
  const [caps, setCaps] = useState<string[]>([]);
  const [notices, setNotices] = useState<string[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [refreshing, setRefreshing] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState<Filter>('');
  const [selected, setSelected] = useState<Session | null>(null);
  const [tick, setTick] = useState(0);
  const seq = useRef(0);
  const focused = useRef(false);
  // A session the drawer asked to show: opened by the first list read after the ask.
  const want = useRef<{ id: string; after: number } | null>(null);

  useEffect(() => {
    let live = true;
    connect(store, conn.id).then((x) => live && setPC(x));
    return () => {
      live = false;
    };
  }, [conn.id]);

  const loadInfo = useCallback(async () => {
    if (!pc) return;
    try {
      const i = await pc.client.info();
      setInfo({ state: 'ok', version: i.version });
      setCaps(i.capabilities);
      // Only the AIs whose sessions this list shows; one that only
      // answers questions is reported in the connection check.
      setNotices(i.agents.filter((a) => a.capabilities.includes('list')).flatMap((a) => a.notices ?? []));
    } catch (e) {
      setInfo({ state: 'error', cause: cause(e) });
      setCaps([]);
    }
  }, [pc]);

  const loadFolders = useCallback(async () => {
    if (!pc) return;
    try {
      setFolders((await pc.client.folders()).folders);
    } catch {
      setFolders([]); // the folders screen says why
    }
  }, [pc]);

  useEffect(() => {
    loadInfo();
  }, [loadInfo]);

  // Typing settles for a moment before the PC is asked.
  useEffect(() => {
    const timer = setTimeout(() => setQ(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  const settleWant = useCallback((mine: number, got: Session[] | null) => {
    const w = want.current;
    if (!w || mine <= w.after) return;
    want.current = null;
    const hit = got?.find((x) => x.id === w.id);
    if (hit) setSelected(hit);
    router.setParams({ session: undefined });
  }, []);

  const fetchSessions = useCallback(async () => {
    if (!pc) return;
    const mine = ++seq.current;
    // Only a list with no search and no state filter tells which sessions wait.
    const full = !q && (filter === '' || filter === 'ask');
    try {
      const state = filter === 'ask' ? undefined : filter || undefined;
      const r = await pc.client.sessions({ q, state });
      if (full) publishSessions(pc.conn.id, r.sessions);
      if (mine !== seq.current) return; // a newer search answered first
      setSessions(r.sessions.filter((x) => (filter === 'ask' ? isAsk(x) : !isAsk(x))));
      setWarnings(r.warnings ?? []);
      setLoad({ state: 'ok' });
      settleWant(mine, r.sessions);
    } catch (e) {
      publishError(pc.conn.id, e);
      if (mine !== seq.current) return;
      setLoad({ state: 'error', cause: cause(e), text: errorText(e) });
      settleWant(mine, null);
    }
    // tick asks again with the same search (the drawer's session param).
  }, [pc, q, filter, tick, settleWant]);

  // The drawer's waiting session: show every session, read the list again,
  // then open that session's sheet and clear the param.
  useEffect(() => {
    if (!wantId) return;
    want.current = { id: wantId, after: seq.current };
    setFilter('');
    setQuery('');
    setQ('');
    setSearchOpen(false);
    setTick((n) => n + 1);
  }, [wantId]);

  // On focus, also on coming back from a new session or the folders screen.
  useFocusEffect(
    useCallback(() => {
      focused.current = true;
      fetchSessions();
      return () => {
        focused.current = false;
      };
    }, [fetchSessions]),
  );
  useFocusEffect(
    useCallback(() => {
      loadFolders();
    }, [loadFolders]),
  );

  // Coming back from the Claude app: states have likely changed.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (st) => {
      if (st === 'active' && focused.current) fetchSessions();
    });
    return () => sub.remove();
  }, [fetchSessions]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([fetchSessions(), loadInfo(), loadFolders()]);
    } finally {
      setRefreshing(false);
    }
  };

  const replace = (next: Session) => {
    setSessions((xs) => xs.map((x) => (x.id === next.id ? next : x)));
    setSelected((cur) => (cur && cur.id === next.id ? next : cur));
  };

  const toggleSearch = () => {
    if (searchOpen) setQuery('');
    setSearchOpen(!searchOpen);
  };

  const goFolders = () => router.push({ pathname: '/pc/[id]/folders', params: { id: conn.id } });
  // A failed list read says more than the first info read.
  const reach: Reach = load.state === 'error' ? { state: 'error', cause: load.cause } : info;
  // 'New session' only once the PC answered (UX-4): before, or while it does
  // not answer, it could only fail, beside the error's own action.
  const canStart = info.state === 'ok' && reach.state !== 'error' && (caps.length === 0 || caps.includes('start'));
  const openFolders = folders.filter((f) => f.server.enabled);

  const top = (
    <View style={s.top}>
      <View style={s.pcBlock}>
        <PCTile size={44} />
        <View style={s.pcNames}>
          <Text style={s.pcName} numberOfLines={1} accessibilityRole="header">
            {conn.name}
          </Text>
          <ReachLine reach={reach} long />
        </View>
        <IconButton name="switch" label={t('pc.pick')} onPress={onPick} size={20} />
      </View>

      {pc === null ? <Text style={s.notice}>{t('sessions.gone')}</Text> : null}
      {load.state === 'error' ? (
        <View style={s.pad}>
          <ReachError cause={load.cause} detail={load.text} onRetry={refresh} />
        </View>
      ) : null}
      {warnings.includes('claude_agents_unavailable') ? <Text style={s.warning}>{t('sessions.agentsUnavailable')}</Text> : null}
      {notices.map((n) => (
        <Text key={n} style={s.notice}>
          {has(`notice.${n}`) ? t(`notice.${n}` as MessageKey) : n}
        </Text>
      ))}

      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.chips} keyboardShouldPersistTaps="handled">
        {filters.map((f) => (
          <Chip
            key={f || 'all'}
            label={f === 'ask' ? t('sessions.askChats') : f ? label('state', f) : t('sessions.all')}
            selected={filter === f}
            onPress={() => setFilter(f)}
          />
        ))}
      </ScrollView>

      <View style={s.label}>
        <SectionLabel text={t('pc.sessions')} />
      </View>
    </View>
  );

  const bottom = (
    <View style={s.foldersBlock}>
      <View style={s.label}>
        <SectionLabel text={t('folders.title')} />
      </View>
      {openFolders.map((f) => {
        const st = f.status?.state ?? 'off';
        const stateKey = `folders.state.${st}`;
        const state = has(stateKey) ? t(stateKey) : st;
        return (
          <Pressable
            key={f.id}
            onPress={goFolders}
            accessibilityRole="button"
            accessibilityLabel={`${f.name || f.id}, ${state}`}
            style={({ pressed }) => [s.row, s.folderRow, pressed && s.pressed]}
          >
            <View style={s.dotBox}>
              <Icon name="folder" size={18} color={th.textSub} />
            </View>
            <View style={s.rowText}>
              <Text style={s.rowTitle} numberOfLines={1}>
                {f.name || f.id}
              </Text>
              <Text style={[s.meta, (st === 'failed' || st === 'untrusted') && { color: th.danger }]} numberOfLines={1}>
                {state}
              </Text>
            </View>
            <Icon name="chevronRight" size={18} color={th.caption} />
          </Pressable>
        );
      })}
      <Pressable
        onPress={goFolders}
        accessibilityRole="button"
        accessibilityLabel={t('pc.keepOpen')}
        style={({ pressed }) => [s.row, s.keepRow, pressed && s.pressed]}
      >
        <View style={s.dotBox}>
          <Icon name="sliders" size={18} color={th.textSub} />
        </View>
        <Text style={[s.rowText, s.keepText]} numberOfLines={1}>
          {t('pc.keepOpen')}
        </Text>
        <Icon name="chevronRight" size={18} color={th.caption} />
      </Pressable>
    </View>
  );

  return (
    <>
      <Header entrance="pcs" right={<IconButton name="search" label={t('pc.search')} onPress={toggleSearch} />} />
      {searchOpen ? (
        <View style={s.searchBox}>
          <Icon name="search" size={18} color={th.caption} />
          <TextInput
            style={s.search}
            value={query}
            onChangeText={setQuery}
            placeholder={t('sessions.search')}
            placeholderTextColor={th.caption}
            accessibilityLabel={t('pc.search')}
            autoFocus
            autoCapitalize="none"
            autoCorrect={false}
            clearButtonMode="while-editing"
            returnKeyType="search"
          />
        </View>
      ) : null}
      <FlatList
        data={sessions}
        keyExtractor={(x) => x.id}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} colors={[th.mark]} tintColor={th.mark} progressBackgroundColor={th.surface} />}
        contentContainerStyle={[s.list, { paddingBottom: 96 + insets.bottom }]}
        keyboardShouldPersistTaps="handled"
        ListHeaderComponent={top}
        ListFooterComponent={pc ? bottom : undefined}
        ListEmptyComponent={
          load.state === 'ok' ? (
            <Text style={s.none}>{q || filter ? t('sessions.emptyFiltered') : t('sessions.empty')}</Text>
          ) : load.state === 'loading' ? (
            <View style={s.loading}>
              <Spinner size={20} />
            </View>
          ) : undefined
        }
        renderItem={({ item }) => <SessionRow session={item} onPress={() => setSelected(item)} />}
      />

      {pc && canStart ? (
        <Pressable
          onPress={() => router.push({ pathname: '/pc/[id]/new', params: { id: conn.id } })}
          accessibilityRole="button"
          accessibilityLabel={t('new.title')}
          style={({ pressed }) => [s.fab, { bottom: 20 + insets.bottom }, pressed && s.fabPressed]}
        >
          <Icon name="plus" size={20} color={th.onFill} />
          <Text style={s.fabText}>{t('new.title')}</Text>
        </Pressable>
      ) : null}

      {pc ? (
        <SessionSheet
          session={selected}
          client={pc.client}
          capabilities={caps}
          onClose={() => setSelected(null)}
          onChanged={replace}
          onRemoved={(gone) => {
            setSelected(null);
            setSessions((xs) => xs.filter((x) => x.id !== gone));
          }}
        />
      ) : null}
    </>
  );
}

function SessionRow({ session, onPress }: { session: Session; onPress: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const st = sessionStatus(session);
  const when = ago(session.lastActive);
  const title = sessionTitle(session);
  const tone = st.kind === 'waiting' ? th.waitingText : st.kind === 'running' ? th.textSub : th.caption;
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${title}, ${st.text}${when ? ` · ${when}` : ''}`}
      style={({ pressed }) => [s.row, s.sessionRow, pressed && s.pressed]}
    >
      <View style={s.dotBox}>
        <StatusDot kind={st.kind} size={10} halo={st.kind === 'waiting'} />
      </View>
      <View style={s.rowText}>
        <Text style={s.rowTitle} numberOfLines={1}>
          {title}
        </Text>
        <Text style={s.meta} numberOfLines={1}>
          <Text style={{ color: tone }}>{st.text}</Text>
          {when ? ` · ${when}` : ''}
        </Text>
      </View>
      <Icon name="chevronRight" size={18} color={th.caption} />
    </Pressable>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
    empty: { flex: 1, justifyContent: 'center', gap: 10, paddingHorizontal: 24, paddingBottom: 40 },
    emptyTitle: { marginTop: 10, fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18, color: th.text },
    emptyHint: { fontSize: 15, lineHeight: 22.5, color: th.textSub },
    emptyButton: { marginTop: 14 },
    list: { paddingTop: 6 },
    top: { gap: 16 },
    pad: { paddingHorizontal: 16 },
    pcBlock: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingLeft: 20, paddingRight: 8 },
    pcNames: { flex: 1, minWidth: 0, gap: 2 },
    pcName: { fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18, color: th.text },
    warning: { paddingHorizontal: 20, fontSize: 13, lineHeight: 19.5, color: th.caption },
    notice: { paddingHorizontal: 20, fontSize: 14, lineHeight: 20.3, color: th.danger },
    chips: { gap: 4, paddingLeft: 16, paddingRight: 20 },
    label: { paddingHorizontal: 16, marginBottom: 4 },
    row: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingLeft: 20, paddingRight: 12 },
    sessionRow: { minHeight: 64 },
    folderRow: { minHeight: 60 },
    keepRow: { minHeight: 52 },
    pressed: { backgroundColor: th.surface },
    dotBox: { width: 18, alignItems: 'center', justifyContent: 'center' },
    rowText: { flex: 1, minWidth: 0, gap: 2 },
    rowTitle: { fontSize: 16, fontWeight: '500', lineHeight: 22, color: th.text },
    meta: { fontSize: 13, lineHeight: 18, color: th.caption },
    keepText: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.textSub },
    none: { paddingHorizontal: 20, paddingVertical: 16, fontSize: 14, lineHeight: 20.3, color: th.caption },
    loading: { paddingVertical: 20, alignItems: 'center' },
    foldersBlock: { marginTop: 16 },
    searchBox: {
      height: 44,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      marginHorizontal: 16,
      marginBottom: 6,
      paddingLeft: 14,
      paddingRight: 8,
      borderRadius: 22,
      borderWidth: 1,
      borderColor: th.border,
      backgroundColor: th.surface,
    },
    search: { flex: 1, height: 44, paddingVertical: 0, fontSize: 15, color: th.text },
    fab: {
      position: 'absolute',
      right: 16,
      height: 52,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      paddingLeft: 18,
      paddingRight: 22,
      borderRadius: 26,
      backgroundColor: th.fill,
      boxShadow: '0px 6px 18px rgba(0,0,0,0.35)',
    },
    fabPressed: { opacity: 0.85 },
    fabText: { fontSize: 15, fontWeight: '600', lineHeight: 21, color: th.onFill },
  }),
);
