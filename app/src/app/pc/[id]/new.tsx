// New session (F4): pick an allowed folder, a name, model and effort; the
// PC starts a background session with remote control and the link goes to
// the Claude app.

import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Linking, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';

import { type AgentClient, CancelledError } from '@/api/client';
import type { Folder } from '@/api/types';
import { connect } from '@/connections/open';
import { efforts, loadPrefs, models } from '@/connections/prefs';
import { ConnectionStore } from '@/connections/store';
import { errorText, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { Choices } from '@/ui/Choices';
import { FolderPicker } from '@/ui/FolderPicker';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

type Model = (typeof models)[number];
type Effort = (typeof efforts)[number];

export default function NewSession() {
  const th = useTheme();
  const s = styles(th);
  const { id } = useLocalSearchParams<{ id: string }>();
  const [client, setClient] = useState<AgentClient | null>(null);
  const [folders, setFolders] = useState<Folder[] | null>(null);
  const [folder, setFolder] = useState('');
  const [name, setName] = useState('');
  const [model, setModel] = useState<Model>('');
  const [effort, setEffort] = useState<Effort>('');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; error?: boolean } | null>(null);
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    (async () => {
      const pc = await connect(store, id);
      if (!pc) return;
      setClient(pc.client);
      const p = await loadPrefs();
      if ((models as readonly string[]).includes(p.model)) setModel(p.model as Model);
      setEffort(p.effort as Effort);
      try {
        const r = await pc.client.folders();
        setFolders(r.folders);
        if (r.folders.length === 1) setFolder(r.folders[0].id);
      } catch (e) {
        setFolders([]);
        setNote({ text: errorText(e), error: true });
      }
    })();
    return () => abort.current?.abort();
  }, [id]);

  const start = async () => {
    if (!client || !folder) return;
    const ctl = new AbortController();
    abort.current = ctl;
    setBusy(true);
    setNote({ text: t('new.starting') });
    try {
      const r = await client.startSession(
        { folder, name: name.trim(), model: model || undefined, effort: effort || undefined },
        { signal: ctl.signal },
      );
      if (r.remoteUrl) {
        await Linking.openURL(r.remoteUrl).catch(() => undefined);
      }
      router.back();
    } catch (e) {
      setNote(e instanceof CancelledError ? null : { text: errorText(e), error: true });
    } finally {
      setBusy(false);
    }
  };

  return (
    <ScrollView style={s.screen} contentContainerStyle={s.body} keyboardShouldPersistTaps="handled">
      <Text style={s.label}>{t('new.folder')}</Text>
      {folders === null ? (
        <ActivityIndicator color={th.accent} />
      ) : folders.length === 0 ? (
        <Text style={s.muted}>{t('new.noFolders')}</Text>
      ) : (
        <FolderPicker folders={folders} value={folder} onChange={setFolder} />
      )}

      <Text style={s.label}>{t('new.name')}</Text>
      <TextInput
        style={s.input}
        value={name}
        onChangeText={setName}
        placeholder={t('new.namePlaceholder')}
        placeholderTextColor={th.muted}
        maxLength={64}
      />

      <Text style={s.label}>{t('new.model')}</Text>
      <Choices options={models} value={model} onChange={setModel} labelOf={(m) => m || t('new.default')} />

      <Text style={s.label}>{t('new.effort')}</Text>
      <Choices options={efforts} value={effort} onChange={setEffort} labelOf={(e) => e || t('new.default')} />

      {note ? <Text style={note.error ? s.error : s.note}>{note.text}</Text> : null}

      {busy ? (
        <View style={s.busy}>
          <ActivityIndicator color={th.accent} />
          <Button secondary label={t('sheet.cancel')} onPress={() => abort.current?.abort()} />
        </View>
      ) : (
        <Button label={t('new.start')} onPress={start} disabled={!client || !folder} />
      )}
    </ScrollView>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { padding: 16, gap: 10 },
    label: { color: th.text, fontSize: 15, fontWeight: '600', marginTop: 8 },
    muted: { color: th.muted, fontSize: 13 },
    note: { color: th.text, fontSize: 15, lineHeight: 22 },
    error: { color: th.danger, fontSize: 15, lineHeight: 22 },
    input: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 10,
      color: th.text,
      fontSize: 15,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    busy: { gap: 12, marginTop: 8 },
  });
