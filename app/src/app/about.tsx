// About (from Settings › About): the brand (#31): app icon I2-A, the
// wordmark and the slogan, with the one-line description, the version and
// the not-official line. The native
// splash shows only the icon (Android 12+ draws an icon only); the launch
// cover (ui/LaunchCover) adds the wordmark at start, and this is where the
// whole composition lives.

import Constants from 'expo-constants';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';
import { TopBar } from '@/ui/Header';
import { useSafeInsets } from '@/ui/keyboard';
import { font, themed, useTheme } from '@/ui/theme';
import { BrandMark, Wordmark } from '@/ui/Wordmark';

export default function About() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const version = Constants.expoConfig?.version ?? '';
  // Opened from a link with nothing behind it, Back leads to the app's start.
  const back = () => (router.canGoBack() ? router.back() : router.replace('/'));

  return (
    <View style={s.screen}>
      <TopBar onBack={back} />
      <ScrollView style={s.screen} contentContainerStyle={[s.body, { paddingBottom: 24 + insets.bottom }]}>
        <View style={s.brand}>
          <BrandMark size={112} />
          <View style={s.words}>
            <Wordmark size={36} />
            <Text style={s.slogan}>{t('app.slogan')}</Text>
            <Text style={s.tagline}>{t('app.tagline')}</Text>
          </View>
        </View>
        <View style={s.foot}>
          {version ? <Text style={s.caption}>{t('about.version', { v: version })}</Text> : null}
          <Text style={s.caption}>{t('app.notOfficial')}</Text>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    body: { flexGrow: 1, paddingHorizontal: 32 },
    // Board: mark 112, 32 to the wordmark (36), 10 to the slogan (16/24).
    brand: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 32, paddingVertical: 32 },
    words: { alignItems: 'center', gap: 10 },
    slogan: { fontSize: 16, lineHeight: 24, fontWeight: '400', color: th.textSub, textAlign: 'center' },
    tagline: { marginTop: 6, maxWidth: 320, fontSize: 15, lineHeight: 23, fontWeight: '400', color: th.textSub, textAlign: 'center' },
    foot: { alignItems: 'center', gap: 4, paddingTop: 16 },
    caption: { ...font.caption, color: th.caption, textAlign: 'center' },
  }),
);
