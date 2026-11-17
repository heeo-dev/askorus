// A question's photos, large (#14): opened by a press on one of the small
// pictures. The photos lie side by side and are swiped through; the screen
// behind is black, whatever the theme, as a photo viewer's is.

import { useEffect, useRef, useState } from 'react';
import { FlatList, Image, Modal, type NativeScrollEvent, type NativeSyntheticEvent, StatusBar, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import type { TurnPhoto } from '@/ask/photos';
import { t } from '@/i18n';

import { IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { modalBars } from './modalBars';
import { useTheme } from './theme';

export function PhotoViewer({ photos, index, onClose }: { photos: TurnPhoto[]; index: number | null; onClose: () => void }) {
  // The open viewer is its own component, so each opening starts at its photo.
  return index === null || !photos.length ? null : <OpenViewer photos={photos} index={Math.min(Math.max(0, index), photos.length - 1)} onClose={onClose} />;
}

function OpenViewer({ photos, index, onClose }: { photos: TurnPhoto[]; index: number; onClose: () => void }) {
  const th = useTheme();
  const insets = useSafeInsets();
  const { width, height } = useWindowDimensions();
  const [at, setAt] = useState(index);
  const scheme = useRef(th.scheme);
  scheme.current = th.scheme;
  // The app's own bars come back in the theme's look when the viewer goes.
  useEffect(() => () => modalBars(scheme.current), []);
  const onScrolled = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
    const i = Math.round(e.nativeEvent.contentOffset.x / Math.max(1, width));
    setAt(Math.min(Math.max(0, i), photos.length - 1));
  };
  return (
    <Modal animationType="fade" visible onRequestClose={onClose} onShow={() => StatusBar.setBarStyle('light-content')} statusBarTranslucent navigationBarTranslucent>
      <View style={styles.screen} testID="photo-viewer" accessibilityViewIsModal>
        <FlatList
          data={photos}
          keyExtractor={(p) => p.uri}
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
                style={styles.photo}
                resizeMode="contain"
                accessibilityRole="image"
                accessibilityLabel={t('ask.photo.viewer', { n: i + 1, total: photos.length })}
                accessibilityIgnoresInvertColors
              />
            </View>
          )}
        />
        <View style={[styles.top, { paddingTop: insets.top + 4 }]}>
          <IconButton name="close" label={t('sheet.close')} onPress={onClose} color="#FFFFFF" />
          {photos.length > 1 ? (
            <Text style={styles.count} accessibilityLiveRegion="polite">
              {at + 1}/{photos.length}
            </Text>
          ) : null}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#000000' },
  photo: { flex: 1 },
  top: { position: 'absolute', top: 0, left: 0, right: 0, flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 6 },
  count: { fontSize: 15, fontWeight: '600', lineHeight: 20, color: '#FFFFFF' },
});
