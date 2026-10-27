// The sheet that slides up for one session (F3, F5, F6): open it in the
// Claude app, stop it, remove it, or copy the command that continues it on
// the PC.

import * as Clipboard from 'expo-clipboard';
import { useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { type AgentClient, CancelledError } from '@/api/client';
import type { OpenResult, Session } from '@/api/types';
import { errorText, has, label, t } from '@/i18n';

import { Button } from './Button';
import { ago, folderName, sessionTitle } from './format';
import { type Theme, useTheme } from './theme';

type Busy = null | 'open' | 'stop' | 'remove';
type Note = { text: string; error?: boolean };

export function SessionSheet({
  session,
  client,
  capabilities,
  onClose,
  onChanged,
  onRemoved,
}: {
  session: Session | null;
  client: AgentClient;
  capabilities: string[];
  onClose: () => void;
  onChanged: (s: Session) => void;
  onRemoved: (id: string) => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const [busy, setBusy] = useState<Busy>(null);
  const [note, setNote] = useState<Note | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);

  // A new session in the sheet starts clean.
  useEffect(() => {
    abort.current?.abort();
    setBusy(null);
    setNote(null);
    setLink(null);
  }, [session?.id]);

  useEffect(() => () => abort.current?.abort(), []);

  if (!session) return null;
  const can = (c: string) => capabilities.length === 0 || capabilities.includes(c);

  const openLink = async (url: string) => {
    try {
      await Linking.openURL(url);
    } catch (e) {
      setNote({ text: errorText(e), error: true });
    }
  };

  const open = async () => {
    const ctl = new AbortController();
    abort.current = ctl;
    setBusy('open');
    setNote({ text: t('sheet.opening') });
    setLink(null);
    try {
      const r: OpenResult = await client.openSession(session.id, { signal: ctl.signal });
      if (r.session) onChanged(r.session);
      const hintKey = `sheet.hint.${r.hint}`;
      if (r.hint && has(hintKey)) {
        setNote({ text: t(hintKey) });
      } else if (r.remoteUrl) {
        const doneKey = `sheet.opened.${r.action}`;
        setNote(has(doneKey) ? { text: t(doneKey) } : null);
        setLink(r.remoteUrl);
        await openLink(r.remoteUrl);
      } else {
        setNote({ text: errorText(new Error(r.action)), error: true });
      }
    } catch (e) {
      setNote(e instanceof CancelledError ? null : { text: errorText(e), error: true });
    } finally {
      setBusy(null);
    }
  };

  const stop = async () => {
    setBusy('stop');
    setNote(null);
    try {
      const r = await client.stopSession(session.id);
      if (r.session) onChanged(r.session);
      setLink(null); // a stopped session's link shows as archived; opening must wake it
      setNote({ text: t('sheet.stopped') });
    } catch (e) {
      setNote({ text: errorText(e), error: true });
    } finally {
      setBusy(null);
    }
  };

  const remove = () => {
    Alert.alert(t('sheet.removeTitle'), t('sheet.removeBody'), [
      { text: t('sheet.cancel'), style: 'cancel' },
      {
        text: t('sheet.remove'),
        style: 'destructive',
        onPress: async () => {
          setBusy('remove');
          setNote(null);
          try {
            await client.removeSession(session.id);
            onRemoved(session.id);
          } catch (e) {
            setNote({ text: errorText(e), error: true });
            setBusy(null);
          }
        },
      },
    ]);
  };

  const copy = async () => {
    if (!session.attachCommand) return;
    await Clipboard.setStringAsync(session.attachCommand);
    setNote({ text: t('sheet.copied') });
  };

  const close = () => {
    abort.current?.abort();
    onClose();
  };

  const running = session.state === 'running';
  const badges = (session.badges ?? []).map((b) => label('badge', b));

  return (
    <Modal visible transparent animationType="slide" onRequestClose={close}>
      <Pressable style={s.backdrop} onPress={close} accessibilityLabel={t('sheet.close')} />
      <View style={s.sheet}>
        <View style={s.grabber} />
        <ScrollView contentContainerStyle={s.body}>
          <Text style={s.title}>{sessionTitle(session)}</Text>
          <View style={s.chips}>
            <Text style={[s.chip, running && s.chipRunning]}>{label('state', session.state)}</Text>
            {session.activity ? <Text style={s.chip}>{session.activity}</Text> : null}
            {badges.map((b) => (
              <Text key={b} style={s.chip}>
                {b}
              </Text>
            ))}
          </View>

          <Row th={th} name={t('sheet.folder')} value={session.folder && `${folderName(session.folder)}  ·  ${session.folder}`} />
          <Row th={th} name={t('sheet.lastActive')} value={ago(session.lastActive)} />
          {session.model ? <Row th={th} name={t('sheet.model')} value={session.model} /> : null}
          {session.lastPrompt ? <Row th={th} name={t('sheet.lastPrompt')} value={session.lastPrompt} /> : null}

          {session.badges?.includes('folder_missing') ? <Text style={s.note}>{t('sheet.folderMissing')}</Text> : null}
          {note ? <Text style={note.error ? s.error : s.note}>{note.text}</Text> : null}

          {busy === 'open' ? (
            <View style={s.busy}>
              <ActivityIndicator color={th.accent} />
              <Button secondary label={t('sheet.cancel')} onPress={() => abort.current?.abort()} />
            </View>
          ) : (
            <View style={s.actions}>
              {link ? (
                // Asks the PC again rather than reusing the link: the session
                // may have stopped since, and its old link is then archived.
                <Button label={t('sheet.openAgain')} onPress={open} />
              ) : can('open_official') && session.openModes.includes('official') ? (
                <Button label={t('sheet.open')} onPress={open} disabled={busy !== null} />
              ) : null}
              {running && can('stop') ? (
                <Button secondary label={t('sheet.stop')} onPress={stop} disabled={busy !== null} />
              ) : null}
              {can('remove') ? (
                <Button danger label={t('sheet.remove')} onPress={remove} disabled={busy !== null} />
              ) : null}
              {busy === 'stop' || busy === 'remove' ? <ActivityIndicator color={th.accent} /> : null}
            </View>
          )}

          {session.attachCommand ? (
            <View style={s.attach}>
              <Text style={s.rowName}>{t('sheet.attach')}</Text>
              <Text style={s.code} selectable>
                {session.attachCommand}
              </Text>
              <Text style={s.muted}>{t('sheet.attachHint')}</Text>
              <Button secondary label={t('sheet.copy')} onPress={copy} />
            </View>
          ) : null}
        </ScrollView>
      </View>
    </Modal>
  );
}

function Row({ th, name, value }: { th: Theme; name: string; value?: string }) {
  const s = styles(th);
  if (!value) return null;
  return (
    <View style={s.row}>
      <Text style={s.rowName}>{name}</Text>
      <Text style={s.rowValue} numberOfLines={3}>
        {value}
      </Text>
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.35)' },
    sheet: {
      backgroundColor: th.bg,
      borderTopLeftRadius: 16,
      borderTopRightRadius: 16,
      maxHeight: '85%',
      paddingBottom: 24,
    },
    grabber: {
      alignSelf: 'center',
      width: 40,
      height: 4,
      borderRadius: 2,
      backgroundColor: th.border,
      marginTop: 8,
    },
    body: { padding: 16, gap: 12 },
    title: { color: th.text, fontSize: 19, fontWeight: '700' },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
    chip: {
      color: th.muted,
      fontSize: 13,
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 10,
      paddingVertical: 3,
      overflow: 'hidden',
    },
    chipRunning: { color: th.ok, borderColor: th.ok },
    row: { gap: 2 },
    rowName: { color: th.muted, fontSize: 13 },
    rowValue: { color: th.text, fontSize: 15 },
    note: { color: th.text, fontSize: 15, lineHeight: 22 },
    error: { color: th.danger, fontSize: 15, lineHeight: 22 },
    muted: { color: th.muted, fontSize: 13 },
    busy: { gap: 12, alignItems: 'stretch' },
    actions: { gap: 10 },
    attach: {
      gap: 6,
      borderTopColor: th.border,
      borderTopWidth: StyleSheet.hairlineWidth,
      paddingTop: 12,
    },
    code: {
      color: th.text,
      fontFamily: Platform.select({ ios: 'Menlo', default: 'monospace' }),
      fontSize: 13,
      backgroundColor: th.card,
      padding: 10,
      borderRadius: 8,
    },
  });
