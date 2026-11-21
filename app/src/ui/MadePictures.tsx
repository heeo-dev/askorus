// The pictures an AI made in its answer (#14, made pictures), under the
// answer's text: one as wide as the answer, two to four two by two. The text
// never waits for them; a picture still on its way shows a spinner, one
// that could not be taken 'Try again'. A press opens it large, on black as
// a photo viewer is, with 'Save', which puts the picture into the phone's
// pictures as it came (its bytes are not encoded again).

import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { FlatList, Image, Modal, type NativeScrollEvent, type NativeSyntheticEvent, Pressable, StatusBar, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import type { MadeShown } from '@/ask/made';
import { saveMadePicture, savingAvailable } from '@/ask/photoSave';
import { t } from '@/i18n';

import { IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { modalBars } from './modalBars';
import { Spinner } from './controls';
import { themed, useTheme } from './theme';

type Saving = 'idle' | 'busy' | 'saved' | 'failed' | 'unsupported';

/** Takes a picture again that could not be taken; given by the screen that follows the turns. */
export const MadeRetry = createContext<((m: MadeShown) => void) | null>(null);

/** limitText says when the AI's picture limit refreshes, in the phone's own time. */
export function limitText(until?: string): string {
  const at = until ? new Date(until) : null;
  if (!at || Number.isNaN(at.getTime())) return t('made.limit');
  return t('made.limitUntil', { when: at.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) });
}

/** MadePictures shows an answer's pictures, what was left out of them, and the picture limit; nothing when there is none of these. */
export function MadePictures({ made = [], leftOut = 0, limit, name }: { made?: MadeShown[]; leftOut?: number; limit?: { until?: string }; name?: string }) {
  const s = styles(useTheme());
  const [open, setOpen] = useState<number | null>(null);
  if (!made.length && !leftOut && !limit) return null;
  const grid = made.length > 1;
  return (
    <View style={s.box} testID="made-pictures">
      {made.length ? (
        <View style={[s.grid, grid && s.gridMany]}>
          {made.map((m, i) => (
            <Cell key={m.image.id} m={m} n={i + 1} total={made.length} wide={!grid} onOpen={() => setOpen(i)} />
          ))}
        </View>
      ) : null}
      {leftOut > 0 ? <Text style={s.note}>{t('made.leftOut', { n: leftOut })}</Text> : null}
      {limit ? (
        <Text style={s.note} testID="made-limit">
          {limitText(limit.until)}
        </Text>
      ) : null}
      <MadeViewer pictures={made} index={open} name={name} onClose={() => setOpen(null)} />
    </View>
  );
}

function Cell({ m, n, total, wide, onOpen }: { m: MadeShown; n: number; total: number; wide: boolean; onOpen: () => void }) {
  const s = styles(useTheme());
  const retry = useContext(MadeRetry);
  const ratio = m.image.width && m.image.height ? m.image.width / m.image.height : 1;
  const frame = [s.cell, wide ? { aspectRatio: Math.min(Math.max(ratio, 0.5), 2) } : s.cellMany];
  if (m.uri) {
    return (
      <Pressable
        onPress={onOpen}
        accessibilityRole="imagebutton"
        accessibilityLabel={t('made.open', { n, total })}
        style={({ pressed }) => [frame, pressed && s.pressed]}
        testID={`made-${m.image.id}`}
      >
        <Image source={{ uri: m.uri }} style={s.image} resizeMode="cover" accessibilityIgnoresInvertColors />
      </Pressable>
    );
  }
  if (m.failed) {
    return (
      <Pressable
        onPress={() => retry?.(m)}
        disabled={!retry}
        accessibilityRole="button"
        accessibilityLabel={t('made.retryLabel', { n, total })}
        style={({ pressed }) => [frame, s.rest, pressed && s.pressed]}
        testID={`made-retry-${m.image.id}`}
      >
        <Text style={s.retry}>{retry ? t('made.retry') : t('made.gone')}</Text>
      </Pressable>
    );
  }
  return (
    <View style={[frame, s.rest]} accessibilityLabel={t('made.coming')} testID={`made-coming-${m.image.id}`}>
      <Spinner size={18} />
    </View>
  );
}

/** MadeSave is a made picture's 'Save' with what it said, in the row's large view. */
export function MadeSave({ m }: { m: MadeShown }) {
  const th = useTheme();
  const s = styles(th);
  const [saving, setSaving] = useState<Saving>('idle');
  if (!m.uri || !savingAvailable()) return null;
  const save = () => {
    setSaving('busy');
    saveMadePicture(m.uri as string, m.image.type).then(
      () => setSaving('saved'),
      (err: unknown) => setSaving((err as { code?: string } | null)?.code === 'unsupported' ? 'unsupported' : 'failed'),
    );
  };
  const said = saving === 'saved' ? t('picture.saved') : saving === 'failed' ? t('picture.saveFailed') : saving === 'unsupported' ? t('picture.saveUnsupported') : null;
  return (
    <View style={s.tools}>
      <Pressable
        onPress={save}
        disabled={saving === 'busy'}
        accessibilityRole="button"
        accessibilityState={{ disabled: saving === 'busy' }}
        hitSlop={8}
        style={({ pressed }) => [s.tool, pressed && s.pressed]}
        testID="made-row-save"
      >
        <Text style={s.toolText}>{t('picture.save')}</Text>
      </Pressable>
      {said ? (
        <Text style={[s.toolSaid, saving !== 'saved' && { color: th.danger }]} accessibilityLiveRegion="polite">
          {said}
        </Text>
      ) : null}
    </View>
  );
}

/** MadeViewer shows the pictures that were taken large, one at a time; index null keeps it closed. */
export function MadeViewer({ pictures, index, name, onClose }: { pictures: MadeShown[]; index: number | null; name?: string; onClose: () => void }) {
  const kept = pictures.filter((p) => p.uri);
  if (index === null || !kept.length) return null;
  const at = Math.max(0, kept.indexOf(pictures[index] ?? kept[0]));
  return <OpenViewer pictures={kept} index={at} name={name} onClose={onClose} />;
}

function OpenViewer({ pictures, index, name, onClose }: { pictures: MadeShown[]; index: number; name?: string; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const { width, height } = useWindowDimensions();
  const [at, setAt] = useState(index);
  const [saving, setSaving] = useState<Saving>('idle');
  const scheme = useRef(th.scheme);
  scheme.current = th.scheme;
  useEffect(() => () => modalBars(scheme.current), []);
  const onScrolled = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
    const i = Math.min(Math.max(0, Math.round(e.nativeEvent.contentOffset.x / Math.max(1, width))), pictures.length - 1);
    if (i !== at) setSaving('idle');
    setAt(i);
  };
  const p = pictures[at];
  const save = () => {
    if (!p?.uri) return;
    setSaving('busy');
    saveMadePicture(p.uri, p.image.type).then(
      () => setSaving('saved'),
      (err: unknown) => setSaving((err as { code?: string } | null)?.code === 'unsupported' ? 'unsupported' : 'failed'),
    );
  };
  const said = saving === 'saved' ? t('picture.saved') : saving === 'failed' ? t('picture.saveFailed') : saving === 'unsupported' ? t('picture.saveUnsupported') : null;
  return (
    <Modal animationType="fade" visible onRequestClose={onClose} onShow={() => StatusBar.setBarStyle('light-content')} statusBarTranslucent navigationBarTranslucent>
      <View style={s.screen} testID="made-viewer" accessibilityViewIsModal>
        <FlatList
          data={pictures}
          keyExtractor={(m) => m.image.id}
          horizontal
          pagingEnabled
          showsHorizontalScrollIndicator={false}
          initialScrollIndex={index}
          getItemLayout={(_, i) => ({ length: width, offset: width * i, index: i })}
          onMomentumScrollEnd={onScrolled}
          renderItem={({ item, index: i }) => (
            <View style={{ width, height }}>
              <Image
                source={{ uri: item.uri }}
                style={s.full}
                resizeMode="contain"
                accessibilityRole="image"
                accessibilityLabel={t('made.viewer', { n: i + 1, total: pictures.length })}
                accessibilityIgnoresInvertColors
              />
            </View>
          )}
        />
        <View style={[s.top, { paddingTop: insets.top + 4 }]}>
          <IconButton name="close" label={t('sheet.close')} onPress={onClose} color="#FFFFFF" />
          <Text style={s.topText} numberOfLines={1} accessibilityRole="header">
            {name ?? ''}
          </Text>
          {pictures.length > 1 ? (
            <Text style={s.topText} accessibilityLiveRegion="polite">
              {at + 1}/{pictures.length}
            </Text>
          ) : null}
        </View>
        <View style={[s.bottom, { paddingBottom: insets.bottom + 12 }]}>
          {savingAvailable() ? (
            <Pressable
              onPress={save}
              disabled={saving === 'busy'}
              accessibilityRole="button"
              accessibilityState={{ disabled: saving === 'busy' }}
              hitSlop={8}
              style={({ pressed }) => [s.saveButton, pressed && s.pressed]}
              testID="made-save"
            >
              <Text style={s.saveText}>{t('picture.save')}</Text>
            </Pressable>
          ) : null}
          {said ? (
            <Text style={s.said} accessibilityLiveRegion="polite">
              {said}
            </Text>
          ) : null}
        </View>
      </View>
    </Modal>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8, marginTop: 4 },
    grid: { gap: 8 },
    gridMany: { flexDirection: 'row', flexWrap: 'wrap' },
    cell: { width: '100%', borderRadius: 12, overflow: 'hidden', backgroundColor: th.surface },
    cellMany: { width: '48%', aspectRatio: 1 },
    image: { width: '100%', height: '100%' },
    rest: { alignItems: 'center', justifyContent: 'center', minHeight: 96 },
    retry: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.accent },
    pressed: { opacity: 0.8 },
    note: { fontSize: 13, lineHeight: 18, color: th.caption },
    screen: { flex: 1, backgroundColor: '#000000' },
    full: { flex: 1, width: '100%' },
    top: { position: 'absolute', left: 0, right: 0, top: 0, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 6, paddingRight: 16 },
    topText: { flexShrink: 1, fontSize: 15, fontWeight: '600', lineHeight: 20, color: '#FFFFFF' },
    bottom: { position: 'absolute', left: 0, right: 0, bottom: 0, alignItems: 'center', gap: 6 },
    saveButton: { paddingHorizontal: 22, paddingVertical: 10, borderRadius: 22, backgroundColor: 'rgba(255,255,255,0.18)' },
    saveText: { fontSize: 15, fontWeight: '600', lineHeight: 20, color: '#FFFFFF' },
    said: { fontSize: 13, lineHeight: 18, color: '#FFFFFF' },
    tools: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 16 },
    tool: { paddingVertical: 2 },
    toolText: { fontSize: 13, fontWeight: '600', lineHeight: 18, color: th.accent },
    toolSaid: { fontSize: 13, lineHeight: 18, color: th.textSub },
  }),
);
