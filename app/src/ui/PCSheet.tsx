// One paired PC in Settings (S5c): its system and address, this phone's
// name on it, the connection check (each step ✓ or ✗, an optional AI as a
// note), renaming (#53: the name on this phone, on a screen of its own) and
// unpairing, which asks first and also removes this phone on the PC.

import { useEffect, useState } from 'react';
import { Alert, Linking, StyleSheet, Text, View } from 'react-native';

import { aiGuideUrl } from '@/api/agents';
import { AgentError, type AgentClient } from '@/api/client';
import { loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { type Check, diagnose } from '@/connections/diagnose';
import { osName } from '@/connections/label';
import { connect } from '@/connections/open';
import { type Connection, ConnectionStore } from '@/connections/store';
import { t } from '@/i18n';

import { Button } from './Button';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';
import { UpdatePanel } from './UpdatePanel';

const store = new ConnectionStore();

/**
 * unpairPC forgets a PC: this phone's token on the PC (when it answers),
 * then the PC here. It returns true when the PC may still list the phone.
 */
export async function unpairPC(c: Connection): Promise<boolean> {
  const pc = await connect(store, c.id);
  let leftOnPC = false;
  try {
    await pc?.client.forgetSelf();
  } catch (e) {
    // Already gone on the PC is fine; otherwise say what is left.
    leftOnPC = !(e instanceof AgentError && e.code === 'unauthorized');
  }
  await store.remove(c.id);
  // The PC the questions went to is forgotten with it: paired again, it comes back under
  // its id (#53) and must not become the current PC on its own (R-94: only a pick does that).
  const prefs = await loadAskPrefs();
  if (prefs.pc === c.id) await saveAskPrefs({ ...prefs, pc: '' });
  return leftOnPC;
}

export function PCSheet({
  conn,
  label,
  onClose,
  onUnpaired,
  onRename,
}: {
  conn: Connection | null;
  /** The PC in words that tell it from the others (pcLabel); its name when left out. */
  label?: string;
  onClose: () => void;
  onUnpaired: (c: Connection, leftOnPC: boolean) => void;
  onRename?: (c: Connection) => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const [checks, setChecks] = useState<Check[] | 'running' | null>(null);
  // An agent that takes updates from phones (#35) gets the update panel here.
  const [updater, setUpdater] = useState<AgentClient | null>(null);

  const connId = conn?.id;
  useEffect(() => {
    setChecks(null);
    setUpdater(null);
    if (!connId) return;
    let live = true;
    connect(store, connId)
      .then(async (pc) => {
        if (!pc) return;
        // A sheet the person opened to look at the PC: a fresh answer, sharing
        // only a request on its way (#74).
        const info = await pc.client.info(undefined, { fresh: true });
        if (live && info.update) setUpdater(pc.client);
      })
      .catch(() => undefined); // the connection check says why
    return () => {
      live = false;
    };
  }, [connId]);

  if (!conn) return null;

  const check = async () => {
    setChecks('running');
    const pc = await connect(store, conn.id);
    setChecks(pc ? await diagnose(pc.client) : []);
  };

  const unpair = () => {
    Alert.alert(t('settings.unpairTitle', { name: label ?? conn.name }), t('settings.unpairBody'), [
      { text: t('sheet.cancel'), style: 'cancel' },
      {
        text: t('settings.unpair'),
        style: 'destructive',
        onPress: async () => onUnpaired(conn, await unpairPC(conn)),
      },
    ]);
  };

  return (
    <Sheet open onClose={onClose} title={conn.name} subtitle={[osName(conn.os), `${conn.host}:${conn.port}`].filter(Boolean).join(' · ')} testID="pc-sheet">
      <View style={s.body}>
        <Text style={s.caption}>{t('settings.thisPhone', { name: conn.deviceName, id: conn.deviceId })}</Text>
        {updater ? (
          <View style={s.update}>
            <Text style={s.label}>{t('update.title')}</Text>
            <UpdatePanel client={updater} deviceId={conn.deviceId} />
          </View>
        ) : null}
        {Array.isArray(checks) && checks.length ? (
          <View style={s.checks}>
            {checks.map((x, i) => (
              <Text key={i} style={[s.check, { color: x.note ? th.caption : x.ok ? th.ok : th.danger }]}>
                {x.note ? '· ' : x.ok ? '✓ ' : '✗ '}
                {x.text}
              </Text>
            ))}
          </View>
        ) : null}
        {Array.isArray(checks) && checks.some((x) => x.ai) ? (
          <Button variant="outline" icon="open" label={t('diag.aiGuide')} onPress={() => Linking.openURL(aiGuideUrl()).catch(() => undefined)} />
        ) : null}
        <Button variant="outline" icon="retry" label={t('settings.check')} onPress={check} busy={checks === 'running'} />
        {onRename ? <Button variant="outline" label={t('settings.rename')} onPress={() => onRename(conn)} /> : null}
        <Button variant="danger" icon="trash" label={t('settings.unpair')} onPress={unpair} />
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 10, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    checks: { gap: 4, padding: 14, borderRadius: 14, backgroundColor: th.bg },
    update: { gap: 8, padding: 14, borderRadius: 14, backgroundColor: th.bg },
    label: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    check: { fontSize: 14, lineHeight: 20.3 },
  }),
);
