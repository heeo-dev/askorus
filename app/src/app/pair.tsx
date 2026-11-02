// Pairing with a PC (F7, N4; S5c board F-Pair): run one command on the PC,
// scan the QR it shows (the main way) or paste the link printed with it,
// check which PC it is, send the code and wait until the person at the PC
// allows it. Opened from 내 PC, Settings, a PC's 'pair again', or by the
// asksemble://pair link itself. A newly paired PC becomes the current one.

import { CameraView, useCameraPermissions } from 'expo-camera';
import * as Clipboard from 'expo-clipboard';
import * as Device from 'expo-device';
import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Linking, Platform, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { CancelledError } from '@/api/client';
import { cleanDeviceName, pairWithPC } from '@/connections/pair';
import { type PairLink, parsePairLink } from '@/connections/pairLink';
import { ConnectionStore } from '@/connections/store';
import { errorText, t } from '@/i18n';
import { Button } from '@/ui/Button';
import { Spinner } from '@/ui/controls';
import { FormScroll } from '@/ui/FormScroll';
import { TopBar } from '@/ui/Header';
import { Icon, IconButton } from '@/ui/Icon';
import { useSafeInsets } from '@/ui/keyboard';
import { Sheet } from '@/ui/Sheet';
import { pickPC } from '@/ui/shell/pcs';
import { themed, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

const pairCommand = 'asksemble-agent pair';

const tailscaleStore =
  Platform.OS === 'ios'
    ? 'https://apps.apple.com/app/tailscale/id1470499037'
    : 'https://play.google.com/store/apps/details?id=com.tailscale.ipn';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

type Step =
  | { kind: 'start' }
  | { kind: 'scan' }
  | { kind: 'confirm'; link: PairLink }
  | { kind: 'waiting'; link: PairLink }
  | { kind: 'failed'; link: PairLink; text: string };

export default function Pair() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const params = useLocalSearchParams<{ v?: string; host?: string; port?: string; code?: string }>();
  const [step, setStep] = useState<Step>({ kind: 'start' });
  const [pasteOpen, setPasteOpen] = useState(false);
  const [pasted, setPasted] = useState('');
  const [pasteError, setPasteError] = useState('');
  const [copied, setCopied] = useState(false);
  const [tailscaleOpen, setTailscaleOpen] = useState(false);
  const [deviceName, setDeviceName] = useState(() => cleanDeviceName(Device.deviceName ?? Device.modelName));
  const abort = useRef<AbortController | null>(null);
  // Back only when there is somewhere to go back to (not when the link opened
  // the app here). Read once the screen has focus: the router is not ready
  // during the first render, and only knows this screen once it is focused.
  const [canGoBack, setCanGoBack] = useState(false);
  useFocusEffect(
    useCallback(() => {
      try {
        setCanGoBack(router.canGoBack());
      } catch {
        setCanGoBack(false);
      }
    }, []),
  );

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
    const link = parsePairLink(text.trim());
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
      const saved = await pairWithPC(link, deviceName, { store }, { signal: ctl.signal });
      await pickPC(saved.id).catch(() => undefined);
      router.dismissTo('/pcs');
    } catch (e) {
      if (e instanceof CancelledError) {
        setStep({ kind: 'confirm', link });
        return;
      }
      setStep({ kind: 'failed', link, text: errorText(e) });
    }
  };

  // The keyboard's Go key does the same as the button (R-16).
  const pairWithPasted = () => {
    if (pasted.trim() && !accept(pasted)) setPasteError(t('pair.badLink'));
  };

  const paste = async () => {
    const text = await Clipboard.getStringAsync().catch(() => '');
    if (!text) return;
    setPasted(text.trim());
    setPasteError('');
  };

  const copy = async () => {
    await Clipboard.setStringAsync(pairCommand);
    setCopied(true);
  };

  if (step.kind === 'scan') return <Scanner onLink={accept} onCancel={() => setStep({ kind: 'start' })} />;

  return (
    <View style={s.screen}>
      <TopBar brand onBack={canGoBack ? () => router.back() : undefined} />
      <FormScroll style={s.screen} contentContainerStyle={s.body}>
        {step.kind === 'start' && (
          <>
            <View style={s.hero}>
              <View style={s.tiles} importantForAccessibility="no-hide-descendants" accessibilityElementsHidden>
                <View style={s.tile}>
                  <Icon name="phone" size={24} color={th.textSub} />
                </View>
                {/* A dashed line as six dashes: Android draws no dashed border on one side only. */}
                <View style={s.dash}>
                  {[0, 1, 2, 3, 4, 5].map((i) => (
                    <View key={i} style={s.dashPart} />
                  ))}
                </View>
                <View style={s.tile}>
                  <Icon name="pc" size={24} color={th.text} />
                </View>
              </View>
              <View style={s.texts}>
                <Text style={s.display} accessibilityRole="header">
                  {t('pair.title')}
                </Text>
                <Text style={s.intro}>{t('pair.intro')}</Text>
              </View>
            </View>

            <View style={s.steps}>
              <View style={s.step}>
                <StepHead n={1} text={t('pair.step1')} />
                <View style={s.code}>
                  <Text style={s.codeText} selectable>
                    {pairCommand}
                  </Text>
                  <IconButton name={copied ? 'check' : 'copy'} label={t('sheet.copy')} onPress={copy} size={20} color={copied ? th.ok : th.textSub} />
                </View>
                {copied ? <Text style={s.caption}>{t('sheet.copied')}</Text> : null}
              </View>
              <View style={s.step}>
                <StepHead n={2} text={t('pair.step2')} />
                <Button icon="camera" label={t('pair.scan')} onPress={() => setStep({ kind: 'scan' })} />
              </View>
            </View>

            <View style={s.pasteBlock}>
              {pasteOpen ? (
                <>
                  <Text style={s.label}>
                    {t('pair.linkLabel')}
                  </Text>
                  <View style={s.inputRow}>
                    <TextInput
                      style={[s.input, s.inputWithButton]}
                      value={pasted}
                      onChangeText={(v) => {
                        setPasted(v);
                        setPasteError('');
                      }}
                      placeholder="asksemble://pair?…"
                      placeholderTextColor={th.caption}
                      accessibilityLabel={t('pair.linkLabel')}
                      autoCapitalize="none"
                      autoCorrect={false}
                      autoFocus
                      keyboardType="url"
                      returnKeyType="go"
                      onSubmitEditing={pairWithPasted}
                    />
                    <IconButton name="paste" label={t('pair.paste')} onPress={paste} size={20} style={s.pasteButton} />
                  </View>
                  {pasteError ? <Text style={s.error}>{pasteError}</Text> : null}
                  {pasted ? <Button variant="outline" label={t('pair.usePasted')} onPress={pairWithPasted} /> : null}
                </>
              ) : (
                <Button variant="quiet" icon="paste" label={t('pair.pasteLink')} onPress={() => setPasteOpen(true)} style={s.center} />
              )}
            </View>
          </>
        )}

        {step.kind === 'confirm' && (
          <>
            <Text style={s.display} accessibilityRole="header">
              {t('pair.confirmTitle')}
            </Text>
            <View style={s.card}>
              <Text style={s.caption}>{t('pair.pcAddress')}</Text>
              <Text style={s.host}>{step.link.host}</Text>
              <Text style={s.caption}>{t('pair.port', { port: step.link.port })}</Text>
            </View>
            <Text style={s.intro}>{t('pair.confirmBody')}</Text>
            <View style={s.field}>
              <Text style={s.label}>{t('pair.deviceName')}</Text>
              <TextInput
                style={s.input}
                value={deviceName}
                onChangeText={setDeviceName}
                maxLength={64}
                accessibilityLabel={t('pair.deviceName')}
                placeholderTextColor={th.caption}
              />
            </View>
            <Button label={t('pair.send')} onPress={() => send(step.link)} disabled={!deviceName.trim()} />
            <Button variant="outline" label={t('pair.otherQR')} onPress={() => setStep({ kind: 'start' })} />
          </>
        )}

        {step.kind === 'waiting' && (
          <View style={s.waiting}>
            <Spinner size={36} />
            <Text style={s.waitTitle} accessibilityRole="header">
              {t('pair.waitingTitle')}
            </Text>
            <Text style={[s.intro, s.centerText]}>{t('pair.waitingBody')}</Text>
            <Button variant="outline" label={t('pair.cancel')} onPress={() => abort.current?.abort()} style={s.wide} />
          </View>
        )}

        {step.kind === 'failed' && (
          <>
            <Text style={s.display} accessibilityRole="header">
              {t('pair.failedTitle')}
            </Text>
            <Text style={s.error}>{step.text}</Text>
            <Button label={t('pair.retry')} onPress={() => setStep({ kind: 'start' })} />
          </>
        )}
      </FormScroll>

      <View style={[s.foot, { paddingBottom: 16 + insets.bottom }]}>
        <Pressable
          onPress={() => setTailscaleOpen(true)}
          accessibilityRole="button"
          accessibilityLabel={t('pair.tailscaleNote')}
          style={({ pressed }) => [s.note, pressed && s.pressed]}
        >
          <Icon name="shield" size={18} color={th.textSub} />
          <Text style={s.noteText}>{t('pair.tailscaleNote')}</Text>
          <Icon name="chevronRight" size={16} color={th.textSub} />
        </Pressable>
        <Text style={s.caption}>{t('app.notOfficial')}</Text>
      </View>

      <Sheet open={tailscaleOpen} onClose={() => setTailscaleOpen(false)} title={t('pair.tailscaleTitle')} testID="tailscale-sheet">
        <View style={s.sheetBody}>
          <Text style={s.intro}>{t('pair.tailscaleBody')}</Text>
          <Button variant="outline" icon="open" label={t('pair.tailscaleGet')} onPress={() => Linking.openURL(tailscaleStore).catch(() => undefined)} />
        </View>
      </Sheet>
    </View>
  );
}

function StepHead({ n, text }: { n: number; text: string }) {
  const s = styles(useTheme());
  return (
    <View style={s.stepHead}>
      <View style={s.num} importantForAccessibility="no-hide-descendants" accessibilityElementsHidden>
        <Text style={s.numText}>{n}</Text>
      </View>
      <Text style={s.stepText} accessibilityRole="header">
        {text}
      </Text>
    </View>
  );
}

function Scanner({ onLink, onCancel }: { onLink: (text: string) => boolean; onCancel: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const [permission, requestPermission] = useCameraPermissions();
  const [wrong, setWrong] = useState(false);
  const done = useRef(false);

  if (!permission) return <View style={s.screen} />;

  if (!permission.granted) {
    return (
      <View style={s.screen}>
        <TopBar onBack={onCancel} />
        <View style={s.cameraAsk}>
          <Icon name="camera" size={28} color={th.textSub} />
          <Text style={s.intro}>{t('pair.cameraWhy')}</Text>
          {permission.canAskAgain ? (
            <Button label={t('pair.cameraAllow')} onPress={requestPermission} />
          ) : (
            <Button label={t('pair.cameraSettings')} onPress={() => Linking.openSettings()} />
          )}
          <Button variant="outline" label={t('pair.cancel')} onPress={onCancel} />
        </View>
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
      <View style={s.frame} pointerEvents="none" />
      <View style={[s.scanFooter, { bottom: 32 + insets.bottom }]}>
        <Text style={s.scanText}>{wrong ? t('pair.badQR') : t('pair.scanHint')}</Text>
        <Pressable onPress={onCancel} accessibilityRole="button" accessibilityLabel={t('pair.cancel')} style={({ pressed }) => [s.scanCancel, pressed && s.scanPressed]}>
          <Text style={s.scanCancelText}>{t('pair.cancel')}</Text>
        </Pressable>
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { gap: 28, paddingTop: 16, paddingHorizontal: 20, paddingBottom: 24 },
    hero: { gap: 18 },
    tiles: { flexDirection: 'row', alignItems: 'center', gap: 10 },
    tile: { width: 52, height: 52, borderRadius: 14, backgroundColor: th.surface, alignItems: 'center', justifyContent: 'center' },
    dash: { width: 48, height: 2, flexDirection: 'row', justifyContent: 'space-between' },
    dashPart: { width: 5, height: 2, backgroundColor: th.borderStrong },
    texts: { gap: 8 },
    display: { fontSize: 24, fontWeight: '600', lineHeight: 32, letterSpacing: -0.48, color: th.text },
    intro: { fontSize: 15, lineHeight: 23, color: th.textSub },
    steps: { gap: 22 },
    step: { gap: 10 },
    stepHead: { flexDirection: 'row', alignItems: 'center', gap: 10 },
    num: { width: 24, height: 24, borderRadius: 12, backgroundColor: th.raised, alignItems: 'center', justifyContent: 'center' },
    numText: { fontSize: 12, fontWeight: '600', lineHeight: 16, color: th.text },
    stepText: { flex: 1, fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    code: { height: 52, flexDirection: 'row', alignItems: 'center', gap: 8, paddingLeft: 16, paddingRight: 4, borderRadius: 14, backgroundColor: th.surface },
    codeText: { flex: 1, minWidth: 0, fontFamily: mono, fontSize: 15, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    pasteBlock: { gap: 10, marginTop: -12 },
    center: { alignSelf: 'center' },
    label: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    inputRow: { justifyContent: 'center' },
    input: {
      height: 52,
      paddingHorizontal: 16,
      paddingVertical: 0,
      borderRadius: 14,
      borderWidth: 1,
      borderColor: th.border,
      backgroundColor: th.surface,
      fontSize: 15,
      color: th.text,
    },
    inputWithButton: { paddingRight: 52 },
    pasteButton: { position: 'absolute', right: 4, top: 4 },
    error: { fontSize: 15, lineHeight: 22.5, color: th.danger },
    card: { gap: 4, padding: 16, borderRadius: 14, backgroundColor: th.surface },
    host: { fontSize: 17, fontWeight: '600', lineHeight: 23, color: th.text },
    field: { gap: 8 },
    waiting: { alignItems: 'center', gap: 16, paddingTop: 48 },
    waitTitle: { fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18, color: th.text, textAlign: 'center' },
    centerText: { textAlign: 'center' },
    wide: { alignSelf: 'stretch' },
    foot: { gap: 2, paddingHorizontal: 20 },
    note: { minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: 10, marginHorizontal: -8, paddingHorizontal: 8, borderRadius: 22 },
    pressed: { backgroundColor: th.raised },
    noteText: { flex: 1, fontSize: 13, lineHeight: 18.2, color: th.textSub },
    sheetBody: { gap: 14, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    cameraAsk: { flex: 1, justifyContent: 'center', gap: 14, paddingHorizontal: 24 },
    scanner: { flex: 1, backgroundColor: '#000000' },
    frame: {
      position: 'absolute',
      alignSelf: 'center',
      top: '28%',
      width: 240,
      height: 240,
      borderRadius: 24,
      borderWidth: 2,
      borderColor: th.fill,
    },
    scanFooter: { position: 'absolute', left: 16, right: 16, gap: 12 },
    scanText: {
      color: '#FFFFFF',
      fontSize: 15,
      lineHeight: 22,
      textAlign: 'center',
      backgroundColor: 'rgba(0,0,0,0.55)',
      paddingVertical: 10,
      paddingHorizontal: 14,
      borderRadius: 14,
      overflow: 'hidden',
    },
    scanCancel: { height: 48, borderRadius: 24, alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.55)', borderWidth: 1, borderColor: 'rgba(255,255,255,0.4)' },
    scanPressed: { opacity: 0.7 },
    scanCancelText: { fontSize: 15, fontWeight: '500', color: '#FFFFFF' },
  }),
);
