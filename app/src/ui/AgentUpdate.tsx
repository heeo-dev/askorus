// The notice that the PC's agent is older than this app (11-18), or than
// the latest release (#35): an agent that can update itself gets 지금
// 올리기, which opens the update panel in a sheet; an older one gets the
// line to run on the PC, with a copy and a send button, since the line is
// typed on the PC and not on this phone. Put away, it stays away for these
// two versions on this PC.

import * as Clipboard from 'expo-clipboard';
import { useEffect, useState } from 'react';
import { Platform, Share, type StyleProp, StyleSheet, Text, View, type ViewStyle } from 'react-native';

import { agentBehind, appVersion, updateLine, useAgentUpdate } from '@/api/agentUpdate';
import type { AgentClient } from '@/api/client';
import type { UpdateInfo } from '@/api/types';
import { t } from '@/i18n';

import { Button } from './Button';
import { IconButton } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';
import { UpdatePanel } from './UpdatePanel';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

export function AgentUpdate({
  pcId,
  version,
  os,
  update,
  client,
  deviceId,
  checkLatest,
  style,
}: {
  pcId: string | null | undefined;
  version: string | undefined;
  os: string | undefined;
  /** /v1/info's update (#35); an older agent has none. */
  update?: UpdateInfo;
  client?: AgentClient | null;
  /** This phone's id on the PC. */
  deviceId?: string;
  /** Ask the PC for the latest release too (My PCs, part 27); the PC asks GitHub at most every six hours. */
  checkLatest?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const th = useTheme();
  const s = styles(th);
  const app = appVersion();
  const [latest, setLatest] = useState<string | null>(null);
  useEffect(() => {
    setLatest(null);
    if (!checkLatest || !client || !update?.on) return;
    const ac = new AbortController();
    client
      .updateStatus(ac.signal)
      .then((st) => st.newer && st.latest && setLatest(st.latest))
      .catch(() => undefined);
    return () => ac.abort();
  }, [checkLatest, client, update?.on, version]);
  // The newer of the app's version and the latest release.
  const target = latest && agentBehind(app, latest) ? latest : app;
  const { show, putAway } = useAgentUpdate(pcId, version, target);
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  if (!show || !version) return null;
  const self = !!update?.can && !!update.allowed && !!client;
  const { shell, line } = updateLine(os ?? '');
  const copy = async () => {
    await Clipboard.setStringAsync(line);
    setCopied(true);
  };
  return (
    <View style={[s.card, style]} testID="agent-update">
      <Text style={s.title} accessibilityRole="header">
        {t('agentUpdate.title')}
      </Text>
      <Text style={s.body}>{t(target === app ? 'agentUpdate.body' : 'agentUpdate.bodyLatest', { have: version, want: target })}</Text>
      <View style={s.buttons}>
        <Button variant="outline" label={t(self ? 'agentUpdate.now' : 'agentUpdate.how')} onPress={() => setOpen(true)} />
        <Button variant="quiet" label={t('agentUpdate.later')} onPress={putAway} />
      </View>
      <Sheet open={open} onClose={() => setOpen(false)} title={t('agentUpdate.title')} testID="agent-update-sheet">
        {self ? (
          <View style={s.sheetBody}>
            <UpdatePanel client={client ?? null} deviceId={deviceId ?? ''} />
          </View>
        ) : (
          <View style={s.sheetBody}>
            <Text style={s.intro}>{t(shell === 'powershell' ? 'agentUpdate.runWindows' : 'agentUpdate.runUnix')}</Text>
            <View style={s.code}>
              <Text style={s.codeText} selectable>
                {line}
              </Text>
              <IconButton name={copied ? 'check' : 'copy'} label={t('sheet.copy')} onPress={copy} size={20} color={copied ? th.ok : th.textSub} />
            </View>
            {copied ? <Text style={s.caption}>{t('sheet.copied')}</Text> : null}
            <Text style={s.caption}>{t('agentUpdate.keeps')}</Text>
            <Button variant="outline" icon="share" label={t('agentUpdate.send')} onPress={() => Share.share({ message: line }).catch(() => undefined)} />
          </View>
        )}
      </Sheet>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    card: { gap: 8, padding: 16, borderRadius: 16, borderWidth: 1, borderColor: th.softLine, backgroundColor: th.soft },
    title: { fontSize: 15, fontWeight: '600', lineHeight: 21, color: th.text },
    body: { fontSize: 14, lineHeight: 20.3, color: th.textSub },
    buttons: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 8, marginTop: 4 },
    sheetBody: { gap: 14, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    intro: { fontSize: 15, lineHeight: 23, color: th.textSub },
    code: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 10, paddingLeft: 14, paddingRight: 4, borderRadius: 14, backgroundColor: th.raised },
    codeText: { flex: 1, minWidth: 0, fontFamily: mono, fontSize: 13, lineHeight: 19, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
