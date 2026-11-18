// The question as a bubble at the right (S5c, F-Ask-Answering): raised
// colour, the bottom-right corner tucked in. The photos it came with (#14)
// sit in a row above it.

import { StyleSheet, Text, View } from 'react-native';

import type { TurnPhoto } from '@/ask/photos';
import { t } from '@/i18n';

import { PhotoStrip } from './PhotoStrip';
import { themed, useTheme } from './theme';

export function ThreadBubble({ text, photos, onOpenPhoto }: { text: string; photos?: TurnPhoto[]; onOpenPhoto?: (index: number) => void }) {
  const s = styles(useTheme());
  const n = photos?.length ?? 0;
  return (
    <View style={s.column}>
      {photos && n > 0 ? (
        <View accessibilityLabel={n === 1 ? t('ask.photo.questionOne') : t('ask.photo.question', { n })} testID="turn-photos">
          <PhotoStrip photos={photos} onOpen={onOpenPhoto ?? (() => undefined)} size={88} align="right" />
        </View>
      ) : null}
      <View style={s.row}>
        <View style={s.bubble}>
          <Text style={s.text} selectable>
            {text}
          </Text>
        </View>
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    column: { gap: 6 },
    row: { flexDirection: 'row', justifyContent: 'flex-end' },
    bubble: {
      maxWidth: 300,
      paddingVertical: 9,
      paddingHorizontal: 16,
      borderTopLeftRadius: 20,
      borderTopRightRadius: 20,
      borderBottomLeftRadius: 20,
      borderBottomRightRadius: 6,
      backgroundColor: th.raised,
    },
    text: { color: th.text, fontSize: 15, lineHeight: 22.5 },
  }),
);
