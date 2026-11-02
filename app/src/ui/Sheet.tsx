// A sheet from the bottom (S5c, B2-Many-Sheet and F-Sheet-Judges): rounded
// top, a handle, a title with a close button, at most 85% of the screen.
// Tapping the dimmed screen above it closes it too.

import type { ReactNode } from 'react';
import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';

import { IconButton } from './Icon';
import { useSafeInsets } from './keyboard';
import { themed, useTheme } from './theme';

export function Sheet({
  open,
  onClose,
  title,
  subtitle,
  children,
  scroll = true,
  testID,
}: {
  open: boolean;
  onClose: () => void;
  title?: string;
  subtitle?: string;
  children: ReactNode;
  scroll?: boolean;
  testID?: string;
}) {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  if (!open) return null;
  return (
    <Modal transparent animationType="slide" visible onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <View style={s.screen}>
        <Pressable
          style={[StyleSheet.absoluteFill, { backgroundColor: th.scrim }]}
          onPress={onClose}
          accessibilityRole="button"
          accessibilityLabel={t('sheet.close')}
          testID={testID ? `${testID}-scrim` : 'sheet-scrim'}
        />
        <View style={[s.sheet, { paddingBottom: 20 + insets.bottom }]} testID={testID} accessibilityViewIsModal>
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
        </View>
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
