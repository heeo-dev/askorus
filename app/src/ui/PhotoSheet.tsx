// Where a photo for the question comes from (#14): opened by the + button.
// 'Take photo' opens the phone's camera app, 'Choose photos' the phone's
// photo picker (screenshots are there too).

import { StyleSheet, View } from 'react-native';

import { t } from '@/i18n';

import { Icon } from './Icon';
import { Group, Row } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function PhotoSheet({ open, onTake, onPick, onClose }: { open: boolean; onTake: () => void; onPick: () => void; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <Sheet open={open} onClose={onClose} title={t('ask.photo.add')} testID="photo-sheet">
      <View style={s.body}>
        <Group>
          <Row title={t('ask.photo.take')} onPress={onTake} accessibilityRole="button" left={<Icon name="camera" size={20} color={th.textSub} />} />
          <Row title={t('ask.photo.pick')} onPress={onPick} accessibilityRole="button" left={<Icon name="plus" size={20} color={th.textSub} />} />
        </Group>
      </View>
    </Sheet>
  );
}

const styles = themed(() =>
  StyleSheet.create({
    body: { paddingTop: 6, paddingHorizontal: 16, paddingBottom: 8 },
  }),
);
