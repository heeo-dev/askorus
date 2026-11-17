// The photos of a question (#14), as small pictures in a row. Above the
// question being written each has a × that takes it off; beside a question
// that was sent they are only shown. A press on one opens it large
// (PhotoViewer).

import { Image, Pressable, ScrollView, StyleSheet, View } from 'react-native';

import type { TurnPhoto } from '@/ask/photos';
import { t } from '@/i18n';

import { Icon } from './Icon';
import { themed, useTheme } from './theme';

export function PhotoStrip({
  photos,
  onOpen,
  onRemove,
  size = 64,
  align = 'left',
}: {
  photos: TurnPhoto[];
  onOpen: (index: number) => void;
  /** Left out, the photos cannot be taken off (a question that was sent). */
  onRemove?: (index: number) => void;
  size?: number;
  align?: 'left' | 'right';
}) {
  const th = useTheme();
  const s = styles(th);
  if (!photos.length) return null;
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      keyboardShouldPersistTaps="handled"
      style={s.scroll}
      contentContainerStyle={[s.row, align === 'right' && s.right]}
      testID="photo-strip"
    >
      {photos.map((p, i) => (
        <View key={p.uri} style={{ width: size, height: size }}>
          <Pressable
            onPress={() => onOpen(i)}
            accessibilityRole="imagebutton"
            accessibilityLabel={t('ask.photo.open', { n: i + 1, total: photos.length })}
            style={({ pressed }) => [s.thumb, pressed && { opacity: 0.8 }]}
          >
            <Image source={{ uri: p.uri }} style={s.image} resizeMode="cover" accessibilityIgnoresInvertColors />
          </Pressable>
          {onRemove ? (
            <Pressable
              onPress={() => onRemove(i)}
              accessibilityRole="button"
              accessibilityLabel={t('ask.photo.remove', { n: i + 1 })}
              hitSlop={10}
              style={({ pressed }) => [s.remove, pressed && { opacity: 0.8 }]}
            >
              <Icon name="close" size={12} color={th.onFill} />
            </Pressable>
          ) : null}
        </View>
      ))}
    </ScrollView>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    scroll: { flexGrow: 0 },
    row: { flexDirection: 'row', gap: 8, paddingVertical: 2 },
    right: { flexGrow: 1, justifyContent: 'flex-end' },
    thumb: { flex: 1, borderRadius: 12, overflow: 'hidden', borderWidth: StyleSheet.hairlineWidth, borderColor: th.border, backgroundColor: th.raised },
    image: { width: '100%', height: '100%' },
    remove: {
      position: 'absolute',
      top: -4,
      right: -4,
      width: 22,
      height: 22,
      borderRadius: 11,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: th.fill,
      borderWidth: 2,
      borderColor: th.surface,
    },
  }),
);
