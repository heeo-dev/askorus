// The pictures the AIs drew for one question, in a row under the combined
// answer (#14, the maintainer's decisions 6 and 10 of 2026-11-21). When
// several AIs each drew one, the combined answer used to show a single
// drawing and the others took a press on each AI's card; here every AI's
// drawings lie side by side under its name, and a press opens one large,
// with the next AI's a press away.
//
// A drawing is the SVG of an answer's code block, written again as a
// drawing only (cleanSvg, through drawingsIn). Each small picture is a page
// with no script (svgTilePage) in a web view with MathBlock's locks, and
// nothing in it takes a touch: the press is the tile's. The large one is
// the SvgBlock an answer shows, with its 'Show code' and 'Save'.

import { useEffect, useMemo, useRef, useState } from 'react';
import { Modal, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { WebView } from 'react-native-webview';

import { t } from '@/i18n';

import { Avatar } from './Avatar';
import { IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { drawingsIn } from './Markdown';
import { allowLoad } from './MathBlock';
import { modalBars } from './modalBars';
import { SvgBlock } from './SvgBlock';
import { svgTilePage } from './svg';
import { themed, useTheme } from './theme';

/** One AI's answer, as the row needs it. */
export type PictureSource = { agent: string; name: string; text: string };

/** One picture of the row: the AI that drew it and which of its drawings it is, from 1. */
export type RowPicture = { key: string; agent: string; name: string; n: number; of: number; svg: string; source: string };

/** A row holds this many pictures of one AI, and this many small pictures in all (each is a web view, as in an answer: R-123). */
export const maxPerAI = 4;
export const maxTiles = 6;

/** rowPictures gathers the drawings of the answers, AI by AI in the order given, at most maxPerAI of each. */
export function rowPictures(answers: readonly PictureSource[]): RowPicture[] {
  const out: RowPicture[] = [];
  for (const a of answers) {
    const drawn = drawingsIn(a.text).slice(0, maxPerAI);
    drawn.forEach((d, i) => out.push({ key: `${a.agent}/${i}`, agent: a.agent, name: a.name, n: i + 1, of: drawn.length, svg: d.svg, source: d.source }));
  }
  return out;
}

const everyOrigin = ['*'];
const noDetectors: ['none'] = ['none'];

/**
 * PictureRow shows the drawings of a question's answers under the combined
 * answer; nothing when there is none. live is false for the older questions
 * of a long conversation: their small pictures are not drawn (a web view
 * each would stay for as long as the conversation is open, R-158), and a
 * press still opens the picture large.
 */
export function PictureRow({ answers, live = true }: { answers: readonly PictureSource[]; live?: boolean }) {
  const th = useTheme();
  const s = styles(th);
  // The polls redraw a turn every second or so: the answers are read again only when one changed.
  const stamp = answers.map((a) => `${a.agent}\u0000${a.name}\u0000${a.text}`).join('\u0001');
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const pictures = useMemo(() => rowPictures(answers), [stamp]);
  const [open, setOpen] = useState<number | null>(null);
  if (!pictures.length) return null;
  const shown = pictures.slice(0, maxTiles);
  const more = pictures.length - shown.length;
  return (
    <View style={s.box} testID="picture-row">
      <Text style={s.title} accessibilityRole="header">
        {t('pictures.title')}
        <Text style={s.count}> {t('ask.countN', { n: pictures.length })}</Text>
      </Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={s.strip} contentContainerStyle={s.stripIn}>
        {shown.map((p, i) => (
          <Pressable
            key={p.key}
            onPress={() => setOpen(i)}
            accessibilityRole="imagebutton"
            accessibilityLabel={t('pictures.open', { name: p.name, n: i + 1, total: pictures.length })}
            style={({ pressed }) => [s.tile, pressed && s.pressed]}
            testID={`picture-tile-${p.key}`}
          >
            <View style={[s.frame, !live && s.rest]} pointerEvents="none">
              {live ? <Tile svg={p.svg} /> : <Text style={s.restText}>{t('pictures.show')}</Text>}
            </View>
            <View style={s.by}>
              <Avatar agent={p.agent} />
              <Text style={s.name} numberOfLines={1}>
                {p.of > 1 ? t('pictures.nth', { name: p.name, n: p.n }) : p.name}
              </Text>
            </View>
          </Pressable>
        ))}
        {more > 0 ? (
          <Pressable
            onPress={() => setOpen(maxTiles)}
            accessibilityRole="button"
            accessibilityLabel={t('pictures.moreLabel', { n: more })}
            style={({ pressed }) => [s.tile, s.moreTile, pressed && s.pressed]}
            testID="picture-more"
          >
            <Text style={s.more}>{t('pictures.more', { n: more })}</Text>
          </Pressable>
        ) : null}
      </ScrollView>
      <PictureViewer pictures={pictures} index={open} onClose={() => setOpen(null)} />
    </View>
  );
}

/** One small picture: a page without script, in a web view that takes no touch. */
function Tile({ svg }: { svg: string }) {
  const [failed, setFailed] = useState(false);
  // A drawing the page will not take leaves its tile empty; it never takes the screen down (R-156).
  const page = useMemo(() => {
    try {
      return svgTilePage(svg);
    } catch {
      return null;
    }
  }, [svg]);
  if (failed || page === null) return null;
  const fail = (): void => setFailed(true);
  return (
    <WebView
      source={{ html: page, baseUrl: 'about:blank' }}
      originWhitelist={everyOrigin}
      onShouldStartLoadWithRequest={allowLoad}
      onLoadStart={(e: { nativeEvent: { url: string } }): void => {
        if (!allowLoad(e.nativeEvent)) fail();
      }}
      onError={fail}
      onRenderProcessGone={fail}
      onContentProcessDidTerminate={fail}
      javaScriptEnabled={false}
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
      testID="picture-tile-webview"
    />
  );
}

/** PictureViewer shows one picture of the row large; index null keeps it closed. */
export function PictureViewer({ pictures, index, onClose }: { pictures: RowPicture[]; index: number | null; onClose: () => void }) {
  // The open viewer is its own component, so each opening starts at its picture.
  return index === null || !pictures.length ? null : <OpenViewer pictures={pictures} index={Math.min(Math.max(0, index), pictures.length - 1)} onClose={onClose} />;
}

function OpenViewer({ pictures, index, onClose }: { pictures: RowPicture[]; index: number; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const [at, setAt] = useState(index);
  const scheme = useRef(th.scheme);
  scheme.current = th.scheme;
  useEffect(() => () => modalBars(scheme.current), []);
  const p = pictures[Math.min(at, pictures.length - 1)];
  const many = pictures.length > 1;
  return (
    <Modal animationType="fade" visible onRequestClose={onClose} onShow={() => modalBars(th.scheme)} statusBarTranslucent navigationBarTranslucent>
      <View style={[s.screen, { paddingTop: insets.top + 4, paddingBottom: insets.bottom + 8 }]} testID="picture-viewer" accessibilityViewIsModal>
        <View style={s.top}>
          <IconButton name="close" label={t('sheet.close')} onPress={onClose} />
          <Avatar agent={p.agent} />
          <Text style={s.viewerName} numberOfLines={1} accessibilityRole="header">
            {p.of > 1 ? t('pictures.nth', { name: p.name, n: p.n }) : p.name}
          </Text>
          {many ? (
            <Text style={s.viewerCount} accessibilityLiveRegion="polite">
              {at + 1}/{pictures.length}
            </Text>
          ) : null}
        </View>
        <ScrollView style={s.body} contentContainerStyle={s.bodyIn}>
          {/* A key per picture: each is a page, and its Save, of its own. */}
          <SvgBlock key={p.key} svg={p.svg} source={p.source} />
        </ScrollView>
        {many ? (
          <View style={s.nav}>
            <IconButton name="chevronRight" label={t('pictures.prev')} onPress={() => setAt((i) => Math.max(0, i - 1))} disabled={at === 0} style={styles0.flip} />
            <IconButton name="chevronRight" label={t('pictures.next')} onPress={() => setAt((i) => Math.min(pictures.length - 1, i + 1))} disabled={at >= pictures.length - 1} />
          </View>
        ) : null}
      </View>
    </Modal>
  );
}

const styles0 = StyleSheet.create({
  web: {
    flex: 1,
    backgroundColor: '#ffffff',
    // As SvgBlock: a WebView inside a ScrollView can crash the app while its screen animates away.
    ...Platform.select({ android: { opacity: 0.99 }, default: {} }),
  },
  flip: { transform: [{ scaleX: -1 }] },
});

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 6 },
    title: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    count: { fontWeight: '400' },
    strip: { marginHorizontal: -20 },
    stripIn: { paddingHorizontal: 20, gap: 8 },
    tile: { width: 148, gap: 6 },
    pressed: { opacity: 0.75 },
    // A drawing is made for a white sheet, whatever the app's theme (as in an answer).
    frame: { width: 148, height: 118, borderRadius: 12, overflow: 'hidden', backgroundColor: '#ffffff', borderWidth: StyleSheet.hairlineWidth, borderColor: th.border },
    by: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    name: { flexShrink: 1, fontSize: 13, fontWeight: '600', lineHeight: 18, color: th.text },
    rest: { alignItems: 'center', justifyContent: 'center', backgroundColor: th.surface },
    restText: { fontSize: 13, fontWeight: '600', lineHeight: 18, color: th.accent },
    moreTile: { height: 118, borderRadius: 12, alignItems: 'center', justifyContent: 'center', backgroundColor: th.surface },
    more: { fontSize: 15, fontWeight: '600', lineHeight: 20, color: th.textSub },
    screen: { flex: 1, backgroundColor: th.bg },
    top: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 6, paddingRight: 16 },
    viewerName: { flex: 1, fontSize: 16, fontWeight: '600', lineHeight: 22, color: th.text },
    viewerCount: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.textSub },
    body: { flex: 1 },
    bodyIn: { paddingHorizontal: 16, paddingVertical: 12 },
    nav: { flexDirection: 'row', justifyContent: 'center', gap: 32 },
  }),
);
