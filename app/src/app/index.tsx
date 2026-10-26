import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from 'react-native';

import { AgentClient, baseUrl } from '@/api/client';
import { type Connection, ConnectionStore } from '@/connections/store';
import { errorText, t } from '@/i18n';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

type Reach = { state: 'checking' } | { state: 'ok'; version: string } | { state: 'error'; text: string };

export default function Home() {
  const th = useTheme();
  const s = styles(th);
  const [conns, setConns] = useState<Connection[] | null>(null);
  const [reach, setReach] = useState<Record<string, Reach>>({});
  const [note, setNote] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    const list = await store.list();
    setConns(list);
    setReach(Object.fromEntries(list.map((c) => [c.id, { state: 'checking' } as Reach])));
    await Promise.all(
      list.map(async (c) => {
        const token = await store.token(c.id);
        const client = new AgentClient(baseUrl(c.host, c.port), { token: token ?? undefined });
        let r: Reach;
        try {
          const info = await client.info();
          r = { state: 'ok', version: info.version };
        } catch (e) {
          r = { state: 'error', text: errorText(e) };
        }
        setReach((prev) => ({ ...prev, [c.id]: r }));
      }),
    );
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await load();
    } finally {
      setRefreshing(false);
    }
  };

  const pairButton = (
    <Pressable style={s.button} onPress={() => setNote(t('home.pairComing'))} accessibilityRole="button">
      <Text style={s.buttonText}>{t('home.pair')}</Text>
    </Pressable>
  );

  return (
    <View style={s.screen}>
      <FlatList
        data={conns ?? []}
        keyExtractor={(c) => c.id}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
        contentContainerStyle={s.list}
        ListEmptyComponent={
          conns === null ? undefined : (
            <View style={s.empty}>
              <Text style={s.title}>{t('home.empty')}</Text>
              <Text style={s.muted}>{t('home.emptyHint')}</Text>
            </View>
          )
        }
        renderItem={({ item }) => {
          const r = reach[item.id] ?? { state: 'checking' };
          return (
            <View style={s.card}>
              <Text style={s.title}>{item.name}</Text>
              <Text style={s.muted}>{item.host}</Text>
              <Text style={r.state === 'error' ? s.error : r.state === 'ok' ? s.ok : s.muted}>
                {r.state === 'checking'
                  ? t('home.checking')
                  : r.state === 'ok'
                    ? t('home.online', { version: r.version })
                    : r.text}
              </Text>
            </View>
          );
        }}
        ListFooterComponent={
          <View style={s.footer}>
            {pairButton}
            {note ? <Text style={s.muted}>{note}</Text> : null}
            <Text style={s.small}>{t('app.notOfficial')}</Text>
          </View>
        }
      />
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    list: { padding: 16, gap: 12 },
    empty: { paddingVertical: 32, gap: 8 },
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 16,
      gap: 4,
    },
    title: { color: th.text, fontSize: 17, fontWeight: '600' },
    muted: { color: th.muted, fontSize: 14 },
    small: { color: th.muted, fontSize: 12, marginTop: 24 },
    ok: { color: th.ok, fontSize: 14 },
    error: { color: th.danger, fontSize: 14 },
    footer: { gap: 12, marginTop: 8 },
    button: { backgroundColor: th.accent, borderRadius: 10, paddingVertical: 14, alignItems: 'center' },
    buttonText: { color: th.accentText, fontSize: 16, fontWeight: '600' },
  });
