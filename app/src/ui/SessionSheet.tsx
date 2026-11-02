// The sheet that slides up for one session (F3, F5, F6; S5c board
// F-PC-Session): its state, folder, model and badges, the last question and
// answer (F8), open it in the Claude app, stop it, remove it, or copy the
// command that continues it on the PC.

import * as Clipboard from 'expo-clipboard';
import { useEffect, useRef, useState } from 'react';
import { Alert, Linking, Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { type AgentClient, CancelledError } from '@/api/client';
import type { OpenResult, Preview, Session } from '@/api/types';
import { errorText, has, label, t } from '@/i18n';

import { Avatar } from './Avatar';
import { Button } from './Button';
import { Spinner, StatusDot } from './controls';
import { ago, folderName, sessionStatus, sessionTitle } from './format';
import { Icon, IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { SheetRise } from './Sheet';
import { themed, useTheme } from './theme';

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
  const insets = useSafeInsets();
  const [busy, setBusy] = useState<Busy>(null);
  const [note, setNote] = useState<Note | null>(null);
  const [copied, setCopied] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);

  // A new session in the sheet starts clean.
  useEffect(() => {
    abort.current?.abort();
    setBusy(null);
    setNote(null);
    setCopied(false);
    setLink(null);
  }, [session?.id]);

  // The last question and answer (F8). Shown while the sheet is open and
  // never stored; when it cannot be read the sheet simply goes without.
  const canPreview = capabilities.includes('preview');
  useEffect(() => {
    setPreview(null);
    if (!session || !canPreview) return;
    const ctl = new AbortController();
    client
      .preview(session.id, ctl.signal)
      .then((p) => !ctl.signal.aborted && setPreview(p.turns.length ? p : null))
      .catch(() => undefined);
    return () => ctl.abort();
  }, [session?.id, canPreview, client]);

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
    setCopied(true);
  };

  const close = () => {
    abort.current?.abort();
    onClose();
  };

  const status = sessionStatus(session);
  const when = ago(session.lastActive);
  const folder = folderName(session.folder);
  const badges = (session.badges ?? []).map((b) => label('badge', b));
  const canOpen = can('open_official') && session.openModes.includes('official');
  const canStop = session.state === 'running' && can('stop');
  const canRemove = can('remove');

  return (
    <Modal visible transparent animationType="fade" onRequestClose={close} statusBarTranslucent navigationBarTranslucent>
      <View style={s.screen}>
        <Pressable
          style={[StyleSheet.absoluteFill, { backgroundColor: th.scrim }]}
          onPress={close}
          accessibilityRole="button"
          accessibilityLabel={t('sheet.close')}
          testID="session-sheet-scrim"
        />
        <SheetRise style={[s.sheet, { paddingBottom: 20 + insets.bottom }]} accessibilityViewIsModal testID="session-sheet">
          <View style={s.handle} />
          <ScrollView style={s.scroll} contentContainerStyle={s.body} keyboardShouldPersistTaps="handled">
            <View style={s.top}>
              <View style={s.head}>
                <View style={s.titles}>
                  <Text style={s.title} accessibilityRole="header">
                    {sessionTitle(session)}
                  </Text>
                  <View style={s.statusLine}>
                    <StatusDot kind={status.kind} />
                    <Text style={s.statusText}>
                      <Text style={{ color: status.kind === 'waiting' ? th.waitingText : th.textSub }}>{status.text}</Text>
                      {when ? ` · ${when}` : ''}
                    </Text>
                  </View>
                </View>
                <IconButton name="close" label={t('sheet.close')} onPress={close} />
              </View>
              {folder || session.model || badges.length ? (
                <View style={s.tags}>
                  {folder ? (
                    <View style={s.tag} accessible accessibilityLabel={`${t('sheet.folder')} ${folder}`}>
                      <Icon name="folder" size={14} color={th.textSub} />
                      <Text style={s.tagText} numberOfLines={1}>
                        {folder}
                      </Text>
                    </View>
                  ) : null}
                  {session.model ? (
                    <View style={s.tag} accessible accessibilityLabel={`${t('sheet.model')} ${session.model}`}>
                      <Text style={s.tagText} numberOfLines={1}>
                        {session.model}
                      </Text>
                    </View>
                  ) : null}
                  {badges.map((b) => (
                    <View key={b} style={s.tag}>
                      <Text style={s.tagText} numberOfLines={1}>
                        {b}
                      </Text>
                    </View>
                  ))}
                </View>
              ) : null}
            </View>

            {preview ? (
              <Recent agent={session.agent} preview={preview} />
            ) : session.lastPrompt ? (
              <View style={s.section}>
                <Text style={s.sectionTitle} accessibilityRole="header">
                  {t('sheet.lastPrompt')}
                </Text>
                <View style={s.card}>
                  <Bubble text={session.lastPrompt} />
                </View>
              </View>
            ) : null}

            {session.badges?.includes('folder_missing') ? <Text style={s.note}>{t('sheet.folderMissing')}</Text> : null}
            {note ? (
              <View style={s.noteRow}>
                {busy === 'open' ? <Spinner size={16} /> : null}
                <Text style={[s.noteText, note.error && s.error]}>{note.text}</Text>
              </View>
            ) : null}

            <View style={s.actions}>
              {busy === 'open' ? (
                <Button variant="outline" label={t('sheet.cancel')} onPress={() => abort.current?.abort()} />
              ) : link ? (
                // Asks the PC again rather than reusing the link: the session
                // may have stopped since, and its old link is then archived.
                <Button icon="open" label={t('sheet.openAgain')} onPress={open} disabled={busy !== null} />
              ) : canOpen ? (
                <Button icon="open" label={t('sheet.open')} onPress={open} disabled={busy !== null} />
              ) : null}
              {canStop || canRemove ? (
                <View style={s.grid}>
                  {canStop ? (
                    <Button
                      variant="outline"
                      icon="stop"
                      label={t('sheet.stop')}
                      onPress={stop}
                      disabled={busy !== null}
                      busy={busy === 'stop'}
                      style={s.cell}
                    />
                  ) : null}
                  {canRemove ? (
                    <Button
                      variant="danger"
                      icon="trash"
                      label={t('sheet.remove')}
                      onPress={remove}
                      disabled={busy !== null}
                      busy={busy === 'remove'}
                      style={s.cell}
                    />
                  ) : null}
                </View>
              ) : null}
            </View>

            {session.attachCommand ? (
              <View>
                <Pressable
                  onPress={copy}
                  accessibilityRole="button"
                  accessibilityLabel={t('sheet.copyAttach')}
                  accessibilityHint={t('sheet.attachHint')}
                  style={({ pressed }) => [s.copyRow, pressed && s.pressed]}
                >
                  <Icon name="terminal" size={18} color={th.textSub} />
                  <Text style={s.copyText}>{t('sheet.copyAttach')}</Text>
                  <Icon name={copied ? 'check' : 'copy'} size={18} color={copied ? th.ok : th.textSub} />
                </Pressable>
                {copied ? <Text style={s.copied}>{t('sheet.copied')}</Text> : null}
              </View>
            ) : null}
          </ScrollView>
        </SheetRise>
      </View>
    </Modal>
  );
}

// "Tools: ran commands 3 · read files 2", or the total from an older agent.
function toolsText(turn: Preview['turns'][number]): string {
  if (!turn.toolKinds?.length) return t('sheet.tools', { n: turn.tools ?? 0 });
  return t('sheet.toolsBy', { list: turn.toolKinds.map((k) => `${label('tool', k.kind)} ${k.count}`).join(' · ') });
}

function Bubble({ text }: { text: string }) {
  const s = styles(useTheme());
  return (
    <View style={s.bubbleRow}>
      <View style={s.bubble}>
        <Text style={s.bubbleText} accessibilityLabel={`${t('sheet.you')}: ${text}`} selectable>
          {text}
        </Text>
      </View>
    </View>
  );
}

// The last question as a bubble on the right, the answer under the AI's
// avatar with the tools it used (F-PC-Session 최근 대화).
function Recent({ agent, preview }: { agent: string; preview: Preview }) {
  const s = styles(useTheme());
  const answered = preview.turns.some((x) => x.role === 'assistant');
  return (
    <View style={s.section}>
      <Text style={s.sectionTitle} accessibilityRole="header">
        {t('sheet.recent')}
      </Text>
      <View style={s.card}>
        {preview.turns.map((turn, i) =>
          turn.role === 'user' ? (
            turn.text ? <Bubble key={i} text={turn.text} /> : null
          ) : (
            <View key={i} style={s.answer}>
              <View style={s.avatar}>
                <Avatar agent={agent} />
              </View>
              <View style={s.answerText}>
                {turn.text ? (
                  <Text style={s.answerBody} accessibilityLabel={`${agentName(agent)}: ${turn.text}`} selectable>
                    {turn.text}
                  </Text>
                ) : null}
                {turn.tools ? <Text style={s.tools}>{toolsText(turn)}</Text> : null}
              </View>
            </View>
          ),
        )}
        {!answered ? <Text style={s.tools}>{t('sheet.noAnswerYet')}</Text> : null}
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, justifyContent: 'flex-end' },
    sheet: {
      maxHeight: '85%',
      paddingTop: 8,
      borderTopLeftRadius: 24,
      borderTopRightRadius: 24,
      backgroundColor: th.surface,
    },
    handle: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, backgroundColor: th.borderStrong },
    scroll: { flexGrow: 0, flexShrink: 1 },
    body: { gap: 16, paddingTop: 12 },
    top: { gap: 10 },
    head: { flexDirection: 'row', alignItems: 'flex-start', gap: 12, paddingLeft: 20, paddingRight: 6 },
    titles: { flex: 1, minWidth: 0, gap: 3, paddingTop: 6 },
    title: { fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18, color: th.text },
    statusLine: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    statusText: { flexShrink: 1, fontSize: 13, lineHeight: 18, color: th.caption },
    tags: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, paddingHorizontal: 20 },
    tag: { height: 26, maxWidth: '100%', flexDirection: 'row', alignItems: 'center', gap: 5, paddingHorizontal: 10, borderRadius: 13, backgroundColor: th.raised },
    tagText: { flexShrink: 1, fontSize: 13, lineHeight: 18, color: th.textSub },
    section: { gap: 8, paddingHorizontal: 20 },
    sectionTitle: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    card: { gap: 12, padding: 14, borderRadius: 16, backgroundColor: th.bg },
    bubbleRow: { flexDirection: 'row', justifyContent: 'flex-end' },
    bubble: {
      maxWidth: 260,
      paddingVertical: 8,
      paddingHorizontal: 14,
      borderTopLeftRadius: 18,
      borderTopRightRadius: 18,
      borderBottomRightRadius: 6,
      borderBottomLeftRadius: 18,
      backgroundColor: th.raised,
    },
    bubbleText: { fontSize: 14, lineHeight: 21, color: th.text },
    answer: { flexDirection: 'row', alignItems: 'flex-start', gap: 10 },
    avatar: { marginTop: 1 },
    answerText: { flex: 1, minWidth: 0, gap: 4 },
    answerBody: { fontSize: 14, lineHeight: 21.7, color: th.text },
    tools: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    note: { paddingHorizontal: 20, fontSize: 14, lineHeight: 20.3, color: th.textSub },
    noteRow: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 20 },
    noteText: { flex: 1, fontSize: 14, lineHeight: 20.3, color: th.textSub },
    error: { color: th.danger },
    actions: { gap: 8, paddingHorizontal: 20 },
    grid: { flexDirection: 'row', gap: 8 },
    cell: { flex: 1 },
    copyRow: { minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 20, marginTop: -4 },
    copyText: { flex: 1, fontSize: 14, lineHeight: 20, color: th.textSub },
    copied: { paddingHorizontal: 20, paddingLeft: 50, fontSize: 13, lineHeight: 18, color: th.caption },
    pressed: { backgroundColor: th.raised },
  }),
);
