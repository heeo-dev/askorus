// The second entrance (extension principle 9): ask several AIs at once.
// A placeholder until Phase 2; hidden while features.ask is false.

import { StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';
import { type Theme, useTheme } from '@/ui/theme';

export default function Ask() {
  const s = styles(useTheme());
  return (
    <View style={s.screen}>
      <Text style={s.text}>{t('ask.soon')}</Text>
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg, padding: 24, justifyContent: 'center' },
    text: { color: th.muted, fontSize: 15, lineHeight: 22, textAlign: 'center' },
  });
