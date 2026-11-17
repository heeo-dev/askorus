// A picture an AI drew as SVG code (#14), shown as a picture. The page that
// draws it is built in svg.ts: it runs none of the SVG's own code and can
// reach no address. Under the picture, 'Show code' shows what was written
// (the code block it would otherwise be) and 'Save' puts a PNG of it into
// the phone's pictures. Any trouble drawing shows the code instead.
//
// The WebView is the one MathBlock uses, with the same locks: only the page
// built here loads, and no URL is handed to another app.

import { useMemo, useRef, useState } from 'react';
import { Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { WebView, type WebViewMessageEvent } from 'react-native-webview';

import { savePicture, savingAvailable } from '@/ask/photoSave';
import { t } from '@/i18n';

import { allowLoad, newNonce } from './MathBlock';
import { svgMessage, svgPage } from './svg';
import { type Theme, useTheme } from './theme';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

// Until the page has measured itself.
const startHeight = 160;
// The long edge of the saved PNG.
const saveEdge = 2048;

const everyOrigin = ['*'];
const noDetectors: ['none'] = ['none'];

type Saving = 'idle' | 'busy' | 'saved' | 'failed' | 'unsupported';

export function SvgBlock({ svg, source }: { svg: string; source: string }) {
  const th = useTheme();
  const s = styles(th);
  const page = useMemo(() => svgPage(svg, newNonce()), [svg]);
  const [height, setHeight] = useState(startHeight);
  const [failed, setFailed] = useState<string | null>(null); // the page the WebView could not show
  const [code, setCode] = useState(false);
  const [saving, setSaving] = useState<Saving>('idle');
  // The WebView, for the one thing asked of it: to run the page's own PNG function.
  const web = useRef<{ injectJavaScript?: (js: string) => void } | null>(null);

  const onMessage = (e: WebViewMessageEvent): void => {
    const m = svgMessage(e.nativeEvent.data);
    if (!m) return;
    if ('png' in m) {
      savePicture(m.png).then(
        () => setSaving('saved'),
        (err: unknown) => setSaving((err as { code?: string } | null)?.code === 'unsupported' ? 'unsupported' : 'failed'),
      );
    } else if ('pngError' in m) setSaving('failed');
    else setHeight(m.height);
  };

  // Kept as one element so a new height does not render the WebView again.
  const view = useMemo(() => {
    const fail = (): void => setFailed(page);
    return (
      <WebView
        ref={(r: unknown) => {
          web.current = r as { injectJavaScript?: (js: string) => void } | null;
        }}
        source={{ html: page, baseUrl: 'about:blank' }}
        originWhitelist={everyOrigin}
        onShouldStartLoadWithRequest={allowLoad}
        onMessage={onMessage}
        onLoadStart={(e: { nativeEvent: { url: string } }): void => {
          if (!allowLoad(e.nativeEvent)) fail();
        }}
        onError={fail}
        onRenderProcessGone={fail}
        onContentProcessDidTerminate={fail}
        setSupportMultipleWindows={false}
        javaScriptCanOpenWindowsAutomatically={false}
        allowFileAccess={false}
        dataDetectorTypes={noDetectors}
        allowsLinkPreview={false}
        setBuiltInZoomControls={false}
        textZoom={100}
        scrollEnabled={false}
        overScrollMode="never"
        showsVerticalScrollIndicator={false}
        showsHorizontalScrollIndicator={false}
        style={styles0.web}
        testID="svg-webview"
      />
    );
    // onMessage only sets state: the first one is as good as any later one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page]);

  const written = (
    <ScrollView horizontal style={s.codeBlock} contentContainerStyle={s.codePad}>
      <Text style={s.codeText} selectable>
        {source}
      </Text>
    </ScrollView>
  );
  if (failed === page) return written;

  const save = (): void => {
    setSaving('busy');
    // The page draws itself on a canvas and sends the PNG back (onMessage).
    web.current?.injectJavaScript?.(`window.askorusPng && window.askorusPng(${saveEdge}); true;`);
  };
  const said = saving === 'saved' ? t('picture.saved') : saving === 'failed' ? t('picture.saveFailed') : saving === 'unsupported' ? t('picture.saveUnsupported') : null;
  return (
    <View style={s.block} testID="svg-block">
      <View style={{ height }} accessibilityRole="image" accessibilityLabel={t('picture.label')}>
        {view}
      </View>
      <View style={s.tools}>
        <Pressable onPress={() => setCode((x) => !x)} accessibilityRole="button" hitSlop={8} style={({ pressed }) => [s.tool, pressed && { opacity: 0.7 }]}>
          <Text style={s.toolText}>{t(code ? 'picture.hideCode' : 'picture.showCode')}</Text>
        </Pressable>
        {savingAvailable() ? (
          <Pressable
            onPress={save}
            disabled={saving === 'busy'}
            accessibilityRole="button"
            accessibilityState={{ disabled: saving === 'busy' }}
            hitSlop={8}
            style={({ pressed }) => [s.tool, pressed && { opacity: 0.7 }]}
          >
            <Text style={s.toolText}>{t('picture.save')}</Text>
          </Pressable>
        ) : null}
        {said ? (
          <Text style={[s.said, saving !== 'saved' && { color: th.danger }]} accessibilityLiveRegion="polite">
            {said}
          </Text>
        ) : null}
      </View>
      {code ? written : null}
    </View>
  );
}

const styles0 = StyleSheet.create({
  web: {
    backgroundColor: 'transparent',
    // Android: a WebView inside a ScrollView can crash the app while its
    // screen animates away; an opacity just under 1 is the usual way round it.
    ...Platform.select({ android: { opacity: 0.99 }, default: {} }),
  },
});

const styles = (th: Theme) =>
  StyleSheet.create({
    block: { gap: 6 },
    tools: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 16 },
    tool: { paddingVertical: 2 },
    toolText: { fontSize: 13, fontWeight: '600', lineHeight: 18, color: th.accent },
    said: { fontSize: 13, lineHeight: 18, color: th.textSub },
    codeBlock: { backgroundColor: th.bg, borderRadius: 8, borderColor: th.border, borderWidth: StyleSheet.hairlineWidth },
    codePad: { padding: 10 },
    codeText: { fontFamily: mono, fontSize: 13, lineHeight: 19, color: th.text },
  });
