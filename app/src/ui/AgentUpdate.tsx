// The notice that the PC's agent is older than this app (11-18): what to
// run on the PC, with a copy and a send button, since the line is typed on
// the PC and not on this phone. Until #35 the phone cannot update the agent
// itself. Put away, it stays away for these two versions on this PC.

import * as Clipboard from 'expo-clipboard';
import { useState } from 'react';
import { Platform, Share, type StyleProp, StyleSheet, Text, View, type ViewStyle } from 'react-native';

import { appVersion, updateLine, useAgentUpdate } from '@/api/agentUpdate';
import { t } from '@/i18n';

import { Button } from './Button';
import { IconButton } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

export function AgentUpdate({
  pcId,
  version,
  os,
  style,
}: {
  pcId: string | null | undefined;
  version: string | undefined;
  os: string | undefined;
  style?: StyleProp<ViewStyle>;
}) {
  const th = useTheme();
  const s = styles(th);
  const app = appVersion();
  const { show, putAway } = useAgentUpdate(pcId, version, app);
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  if (!show || !version) return null;
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
      <Text style={s.body}>{t('agentUpdate.body', { have: version, want: app })}</Text>
      <View style={s.buttons}>
        <Button variant="outline" label={t('agentUpdate.how')} onPress={() => setOpen(true)} />
        <Button variant="quiet" label={t('agentUpdate.later')} onPress={putAway} />
      </View>
      <Sheet open={open} onClose={() => setOpen(false)} title={t('agentUpdate.title')} testID="agent-update-sheet">
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
