// New session (F4; S5c board F-PC-New): pick an allowed or recent folder, a
// name, and under Advanced a model and effort; the PC starts a background
// session with remote control and the link goes to the Claude app. The
// start button sits in a bar at the bottom that rises with the keyboard.

import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';

import { type AgentClient, CancelledError } from '@/api/client';
import { openRemoteUrl } from '@/api/remoteUrl';
import type { Folder } from '@/api/types';
import { connect } from '@/connections/open';
import { efforts, loadPrefs, models } from '@/connections/prefs';
import { ConnectionStore } from '@/connections/store';
import { errorText, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { Segment, Spinner } from '@/ui/controls';
import { knownModel, settingsLine } from '@/ui/DefaultsSheet';
import { FolderPicker } from '@/ui/FolderPicker';
import { FormScroll } from '@/ui/FormScroll';
import { TopBar } from '@/ui/Header';
import { Icon } from '@/ui/Icon';
import { useKeyboardInset, useSafeInsets } from '@/ui/keyboard';
import { SectionLabel } from '@/ui/List';
import { themed, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

type Model = (typeof models)[number];
type Effort = (typeof efforts)[number];

export default function NewSession() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const keyboard = useKeyboardInset();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [pcName, setPCName] = useState('');
  const [client, setClient] = useState<AgentClient | null>(null);
  const [folders, setFolders] = useState<Folder[] | null>(null);
  const [folder, setFolder] = useState('');
  const [name, setName] = useState('');
  const [model, setModel] = useState<Model>('');
  const [effort, setEffort] = useState<Effort>('');
  const [advanced, setAdvanced] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    (async () => {
      const pc = await connect(store, id);
      if (!pc) return;
      setPCName(pc.conn.name);
      setClient(pc.client);
      const p = await loadPrefs();
      setModel(knownModel(p.model));
      setEffort(p.effort as Effort);
      try {
        const r = await pc.client.folders();
        setFolders(r.folders);
        if (r.folders.length === 1) setFolder(r.folders[0].id);
      } catch (e) {
        setFolders([]);
        setNote(errorText(e));
      }
    })();
    return () => abort.current?.abort();
  }, [id]);

  const start = async () => {
    if (!client || !folder) return;
    const ctl = new AbortController();
    abort.current = ctl;
    setBusy(true);
    setNote(null);
    try {
      const r = await client.startSession(
        { folder, name: name.trim(), model: model || undefined, effort: effort || undefined },
        { signal: ctl.signal },
      );
      if (r.remoteUrl) {
        await openRemoteUrl(r.remoteUrl).catch(() => undefined);
      }
      router.back();
    } catch (e) {
      setNote(e instanceof CancelledError ? null : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={s.screen}>
      <TopBar title={t('new.title')} subtitle={pcName} onBack={() => router.back()} />
      <FormScroll style={s.scroll} contentContainerStyle={s.body}>
        <View style={s.field}>
          <SectionLabel text={t('new.folder')} />
          {folders === null ? (
            <View style={s.loading}>
              <Spinner size={20} />
            </View>
          ) : folders.length === 0 ? (
            <Text style={s.caption}>{t('new.noFolders')}</Text>
          ) : (
            <FolderPicker folders={folders} value={folder} onChange={setFolder} />
          )}
        </View>

        <View style={s.field}>
          <SectionLabel text={t('new.name')} />
          <TextInput
            style={s.input}
            value={name}
            onChangeText={setName}
            placeholder={t('new.namePlaceholder')}
            placeholderTextColor={th.caption}
            accessibilityLabel={t('new.name')}
            maxLength={64}
            returnKeyType="done"
          />
        </View>

        <View>
          <Pressable
            onPress={() => setAdvanced(!advanced)}
            accessibilityRole="button"
            accessibilityLabel={`${t('new.advanced')}, ${t('new.advancedHint')}`}
            accessibilityState={{ expanded: advanced }}
            accessibilityValue={{ text: settingsLine(model, effort) }}
            style={({ pressed }) => [s.advanced, pressed && s.pressed]}
          >
            <Icon name="sliders" size={20} color={th.textSub} />
            <View style={s.advancedText}>
              <Text style={s.advancedTitle}>{t('new.advanced')}</Text>
              <Text style={s.caption}>{t('new.advancedHint')}</Text>
            </View>
            <Text style={s.value} numberOfLines={1}>
              {settingsLine(model, effort)}
            </Text>
            <Icon name={advanced ? 'chevronUp' : 'chevronDown'} size={18} color={th.textSub} />
          </Pressable>
          {advanced ? (
            <View style={s.choices}>
              <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                <Segment options={models} value={model} onChange={setModel} labelOf={(m) => m || t('new.default')} look="tier" label={t('new.model')} />
              </ScrollView>
              <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                <Segment options={efforts} value={effort} onChange={setEffort} labelOf={(e) => e || t('new.default')} look="tier" label={t('new.effort')} />
              </ScrollView>
            </View>
          ) : null}
        </View>
      </FormScroll>

      <View style={[s.bar, { paddingBottom: 16 + Math.max(keyboard, insets.bottom) }]}>
        {note ? <Text style={s.error}>{note}</Text> : null}
        {busy ? (
          <>
            <View style={s.busy}>
              <Spinner size={18} />
              <Text style={s.busyText}>{t('new.starting')}</Text>
            </View>
            <Button variant="outline" label={t('sheet.cancel')} onPress={() => abort.current?.abort()} />
          </>
        ) : (
          <>
            <Button label={t('new.start')} onPress={start} disabled={!client || !folder} />
            <Text style={s.hint}>{t('new.startHint')}</Text>
          </>
        )}
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    scroll: { flex: 1 },
    body: { gap: 22, paddingTop: 10, paddingHorizontal: 16, paddingBottom: 16 },
    field: { gap: 8 },
    loading: { paddingVertical: 16, alignItems: 'center' },
    caption: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    input: {
      height: 52,
      paddingHorizontal: 16,
      paddingVertical: 0,
      borderRadius: 16,
      borderWidth: 1,
      borderColor: th.border,
      backgroundColor: th.surface,
      fontSize: 15,
      color: th.text,
    },
    advanced: { minHeight: 60, flexDirection: 'row', alignItems: 'center', gap: 14, paddingLeft: 16, paddingRight: 12, borderRadius: 16, backgroundColor: th.surface },
    pressed: { backgroundColor: th.raised },
    advancedText: { flex: 1, minWidth: 0, gap: 2 },
    advancedTitle: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    value: { flexShrink: 1, maxWidth: '40%', fontSize: 14, lineHeight: 20, color: th.textSub },
    choices: { gap: 10, paddingTop: 12, paddingHorizontal: 4 },
    bar: { gap: 8, paddingTop: 12, paddingHorizontal: 16, borderTopWidth: 1, borderTopColor: th.barLine, backgroundColor: th.bg },
    hint: { textAlign: 'center', fontSize: 13, lineHeight: 18.2, color: th.caption },
    error: { fontSize: 14, lineHeight: 20.3, color: th.danger },
    busy: { minHeight: 52, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 10 },
    busyText: { flexShrink: 1, fontSize: 14, lineHeight: 20.3, color: th.textSub },
  }),
);
