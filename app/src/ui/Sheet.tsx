// A sheet from the bottom (S5c, B2-Many-Sheet and the boards' sheets): rounded
// top, a handle, a title with a close button, at most 85% of the screen.
// Tapping the dimmed screen above it closes it too. The Modal fades, so the
// dimmed screen darkens where it is, and only the sheet slides up (#2: a
// Modal that slides carried the dark screen up from the bottom with it).

import { type ReactNode, useEffect, useRef } from 'react';
import { Animated, Easing, Modal, Pressable, ScrollView, StyleSheet, Text, useWindowDimensions, View, type ViewProps } from 'react-native';

import { t } from '@/i18n';

import { IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { modalBars } from './modalBars';
import { themed, useTheme } from './theme';

type SheetProps = {
  open: boolean;
  onClose: () => void;
  title?: string;
  subtitle?: string;
  children: ReactNode;
  scroll?: boolean;
  testID?: string;
};

export function Sheet(props: SheetProps) {
  // The open sheet is its own component, so each opening starts its slide
  // from the bottom again.
  return props.open ? <OpenSheet {...props} /> : null;
}

/**
 * SheetRise is the sheet's own view: it slides up from below the screen
 * when it mounts, so it belongs inside the open sheet only (a sheet that
 * stays mounted while closed would run its slide for nothing). Its Modal
 * fades; the fade also takes the sheet away on close.
 */
export function SheetRise({ style, children, ...rest }: ViewProps) {
  const { height } = useWindowDimensions();
  const rise = useRef(new Animated.Value(0)).current;
  useEffect(() => {
    const run = Animated.timing(rise, { toValue: 1, duration: 240, easing: Easing.out(Easing.cubic), useNativeDriver: true });
    run.start();
    return () => run.stop();
  }, [rise]);
  return (
    <Animated.View {...rest} style={[style, { transform: [{ translateY: rise.interpolate({ inputRange: [0, 1], outputRange: [height, 0] }) }] }]}>
      {children}
    </Animated.View>
  );
}

function OpenSheet({ onClose, title, subtitle, children, scroll = true, testID }: SheetProps) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  return (
    <Modal
      transparent
      animationType="fade"
      visible
      onRequestClose={onClose}
      onShow={() => modalBars(th.scheme)}
      statusBarTranslucent
      navigationBarTranslucent
    >
      <View style={s.screen}>
        <Pressable
          style={[StyleSheet.absoluteFill, { backgroundColor: th.scrim }]}
          onPress={onClose}
          accessibilityRole="button"
          accessibilityLabel={t('sheet.close')}
          testID={testID ? `${testID}-scrim` : 'sheet-scrim'}
        />
        <SheetRise style={[s.sheet, { paddingBottom: 20 + insets.bottom }]} testID={testID} accessibilityViewIsModal>
          <View style={s.handle} />
          <View style={s.header}>
            <View style={s.titles}>
              {title ? (
                <Text style={s.title} accessibilityRole="header">
                  {title}
                </Text>
              ) : null}
              {subtitle ? <Text style={s.subtitle}>{subtitle}</Text> : null}
            </View>
            <IconButton name="close" label={t('sheet.close')} onPress={onClose} size={22} />
          </View>
          {scroll ? (
            <ScrollView style={s.scroll} keyboardShouldPersistTaps="handled">
              {children}
            </ScrollView>
          ) : (
            children
          )}
        </SheetRise>
      </View>
    </Modal>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, justifyContent: 'flex-end' },
    sheet: {
      maxHeight: '85%',
      paddingTop: 8,
      borderTopLeftRadius: 24,
      borderTopRightRadius: 24,
      backgroundColor: th.surface,
    },
    handle: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, backgroundColor: th.borderStrong },
    header: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingTop: 10, paddingRight: 6, paddingBottom: 4, paddingLeft: 20 },
    titles: { flex: 1, minWidth: 0, gap: 2 },
    title: { fontSize: 17, fontWeight: '600', lineHeight: 23, letterSpacing: -0.17, color: th.text },
    subtitle: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    scroll: { flexGrow: 0, flexShrink: 1 },
  }),
);
