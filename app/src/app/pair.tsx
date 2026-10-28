// Pairing with a PC (F7, N4): scan the QR the PC shows, check which PC it
// is, send the code and wait until the person at the PC allows it. Opened
// from the home screen or by the asksemble://pair link itself.

import { CameraView, useCameraPermissions } from 'expo-camera';
import * as Device from 'expo-device';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Linking,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { CancelledError } from '@/api/client';
import { cleanDeviceName, pairWithPC } from '@/connections/pair';
import { type PairLink, parsePairLink } from '@/connections/pairLink';
import { ConnectionStore } from '@/connections/store';
import { errorText, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { FormScroll } from '@/ui/FormScroll';
import { type Theme, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

const tailscaleStore =
  Platform.OS === 'ios'
    ? 'https://apps.apple.com/app/tailscale/id1470499037'
    : 'https://play.google.com/store/apps/details?id=com.tailscale.ipn';

type Step =
  | { kind: 'start' }
  | { kind: 'scan' }
  | { kind: 'confirm'; link: PairLink }
  | { kind: 'waiting'; link: PairLink }
  | { kind: 'failed'; link: PairLink; text: string };

export default function Pair() {
  const th = useTheme();
  const s = styles(th);
  const params = useLocalSearchParams<{ v?: string; host?: string; port?: string; code?: string }>();
  const [step, setStep] = useState<Step>({ kind: 'start' });
  const [pasted, setPasted] = useState('');
  const [pasteError, setPasteError] = useState('');
  const [deviceName, setDeviceName] = useState(() => cleanDeviceName(Device.deviceName ?? Device.modelName));
  const abort = useRef<AbortController | null>(null);

  // Opened by the asksemble://pair link: start at the confirm step.
  useEffect(() => {
    if (!params.code) return;
    const q = new URLSearchParams({
      v: params.v ?? '',
      host: params.host ?? '',
      port: params.port ?? '',
      code: params.code,
    });
    const link = parsePairLink(`asksemble://pair?${q}`);
    if (link) setStep({ kind: 'confirm', link });
  }, [params.v, params.host, params.port, params.code]);

  useEffect(() => () => abort.current?.abort(), []);

  const accept = (text: string): boolean => {
    const link = parsePairLink(text);
    if (!link) return false;
    setStep({ kind: 'confirm', link });
    return true;
  };

  const send = async (link: PairLink) => {
    abort.current?.abort();
    const ctl = new AbortController();
    abort.current = ctl;
    setStep({ kind: 'waiting', link });
    try {
      await pairWithPC(link, deviceName, { store }, { signal: ctl.signal });
      router.dismissTo('/');
    } catch (e) {
      if (e instanceof CancelledError) {
        setStep({ kind: 'confirm', link });
        return;
      }
      setStep({ kind: 'failed', link, text: errorText(e) });
    }
  };

  if (step.kind === 'scan') return <Scanner onLink={accept} onCancel={() => setStep({ kind: 'start' })} />;

  return (
    <FormScroll style={s.screen} contentContainerStyle={s.body}>
      {step.kind === 'start' && (
        <>
          <Text style={s.heading}>{t('pair.title')}</Text>
          <View style={s.card}>
            <Text style={s.title}>{t('pair.tailscaleTitle')}</Text>
            <Text style={s.text}>{t('pair.tailscaleBody')}</Text>
            <Pressable onPress={() => Linking.openURL(tailscaleStore)} accessibilityRole="link">
              <Text style={s.link}>{t('pair.tailscaleGet')}</Text>
            </Pressable>
          </View>
          <View style={s.card}>
            <Text style={s.title}>{t('pair.pcTitle')}</Text>
            <Text style={s.text}>{t('pair.pcBody')}</Text>
            <Text style={s.code}>asksemble-agent pair</Text>
          </View>
          <Button label={t('pair.scan')} onPress={() => setStep({ kind: 'scan' })} />
          <Text style={s.muted}>{t('pair.pasteHint')}</Text>
          <TextInput
            style={s.input}
            value={pasted}
            onChangeText={(v) => {
              setPasted(v);
              setPasteError('');
            }}
            placeholder="asksemble://pair?…"
            placeholderTextColor={th.muted}
            autoCapitalize="none"
            autoCorrect={false}
          />
          {pasted ? (
            <Button
              secondary
              label={t('pair.usePasted')}
              onPress={() => {
                if (!accept(pasted)) setPasteError(t('pair.badLink'));
              }}
            />
          ) : null}
          {pasteError ? <Text style={s.error}>{pasteError}</Text> : null}
        </>
      )}

      {step.kind === 'confirm' && (
        <>
          <Text style={s.heading}>{t('pair.confirmTitle')}</Text>
          <View style={s.card}>
            <Text style={s.muted}>{t('pair.pcAddress')}</Text>
            <Text style={s.title}>{step.link.host}</Text>
            <Text style={s.muted}>{t('pair.port', { port: step.link.port })}</Text>
          </View>
          <Text style={s.text}>{t('pair.confirmBody')}</Text>
          <Text style={s.muted}>{t('pair.deviceName')}</Text>
          <TextInput
            style={s.input}
            value={deviceName}
            onChangeText={setDeviceName}
            maxLength={64}
            placeholderTextColor={th.muted}
          />
          <Button label={t('pair.send')} onPress={() => send(step.link)} disabled={!deviceName.trim()} />
          <Button secondary label={t('pair.otherQR')} onPress={() => setStep({ kind: 'start' })} />
        </>
      )}

      {step.kind === 'waiting' && (
        <View style={s.waiting}>
          <ActivityIndicator size="large" color={th.accent} />
          <Text style={s.title}>{t('pair.waitingTitle')}</Text>
          <Text style={s.text}>{t('pair.waitingBody')}</Text>
          <Button secondary label={t('pair.cancel')} onPress={() => abort.current?.abort()} />
        </View>
      )}

      {step.kind === 'failed' && (
        <>
          <Text style={s.heading}>{t('pair.failedTitle')}</Text>
          <Text style={s.error}>{step.text}</Text>
          <Button label={t('pair.retry')} onPress={() => setStep({ kind: 'start' })} />
        </>
      )}
    </FormScroll>
  );
}

function Scanner({ onLink, onCancel }: { onLink: (text: string) => boolean; onCancel: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const [permission, requestPermission] = useCameraPermissions();
  const [wrong, setWrong] = useState(false);
  const done = useRef(false);

  if (!permission) return <View style={s.screen} />;

  if (!permission.granted) {
    return (
      <View style={[s.screen, s.body]}>
        <Text style={s.text}>{t('pair.cameraWhy')}</Text>
        {permission.canAskAgain ? (
          <Button label={t('pair.cameraAllow')} onPress={requestPermission} />
        ) : (
          <Button label={t('pair.cameraSettings')} onPress={() => Linking.openSettings()} />
        )}
        <Button secondary label={t('pair.cancel')} onPress={onCancel} />
      </View>
    );
  }

  return (
    <View style={s.scanner}>
      <CameraView
        style={StyleSheet.absoluteFill}
        facing="back"
        barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
        onBarcodeScanned={({ data }) => {
          if (done.current) return;
          if (onLink(data)) done.current = true;
          else setWrong(true);
        }}
      />
      <View style={s.scanFooter}>
        <Text style={s.scanText}>{wrong ? t('pair.badQR') : t('pair.scanHint')}</Text>
        <Button secondary label={t('pair.cancel')} onPress={onCancel} />
      </View>
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { padding: 16, gap: 12 },
    heading: { color: th.text, fontSize: 22, fontWeight: '700', marginBottom: 4 },
    card: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: StyleSheet.hairlineWidth,
      borderRadius: 12,
      padding: 16,
      gap: 6,
    },
    title: { color: th.text, fontSize: 17, fontWeight: '600' },
    text: { color: th.text, fontSize: 15, lineHeight: 22 },
    muted: { color: th.muted, fontSize: 14 },
    error: { color: th.danger, fontSize: 15, lineHeight: 22 },
    link: { color: th.accent, fontSize: 15, fontWeight: '600', marginTop: 4 },
    code: { color: th.text, fontFamily: Platform.select({ ios: 'Menlo', default: 'monospace' }), fontSize: 14 },
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
    waiting: { alignItems: 'center', gap: 16, paddingTop: 48 },
    scanner: { flex: 1, backgroundColor: '#000' },
    scanFooter: { position: 'absolute', left: 16, right: 16, bottom: 32, gap: 12 },
    scanText: {
      color: '#fff',
      fontSize: 15,
      textAlign: 'center',
      backgroundColor: 'rgba(0,0,0,0.55)',
      padding: 10,
      borderRadius: 8,
    },
  });
