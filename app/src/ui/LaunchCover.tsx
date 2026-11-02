// The launch cover (#1): the native splash can draw only the icon (Android
// 12+), so the app opens on the same icon in the same place, in the same
// light or dark as the splash, and the wordmark comes up under it as on
// the board F-Brand-Splash-Final; then the cover fades to the first screen.
// It shows once per start, never takes a touch and is hidden from screen
// readers (the first screen is what they should land on). Where the splash
// drew the icon smaller, the cover's icon starts at that size and grows as
// the wordmark comes up (R-63).

import { useEffect, useRef, useState } from 'react';
import { Animated, Platform, StyleSheet, useColorScheme, View } from 'react-native';

import { palettes, resolveScheme } from './theme';
import { BrandMark, Wordmark } from './Wordmark';

/** The icon's size, as app.json gives the native splash (imageWidth). */
export const LAUNCH_ICON = 112;

/**
 * splashIconScale is how big the native splash drew the icon, as a share of
 * LAUNCH_ICON. Android 12+ draws imageWidth on a 288 dp canvas: stock
 * Android (and the splash library before 12) shows the canvas at 288 dp,
 * Samsung's One UI at half that (the S10 on Android 12, 11-03). iOS draws
 * imageWidth points.
 */
export function splashIconScale(d: { os: string; version: number | string; manufacturer?: string }): number {
  return d.os === 'android' && Number(d.version) >= 31 && /samsung/i.test(d.manufacturer ?? '') ? 0.5 : 1;
}

function deviceSplashIconScale(): number {
  const manufacturer = Platform.OS === 'android' ? Platform.constants.Manufacturer : undefined;
  return splashIconScale({ os: Platform.OS, version: Platform.Version, manufacturer });
}

export function LaunchCover() {
  const scheme = resolveScheme('system', useColorScheme());
  const [done, setDone] = useState(false);
  const word = useRef(new Animated.Value(0)).current;
  const cover = useRef(new Animated.Value(1)).current;
  const start = useRef(deviceSplashIconScale()).current;

  useEffect(() => {
    const run = Animated.sequence([
      Animated.timing(word, { toValue: 1, duration: 280, delay: 80, useNativeDriver: true }),
      Animated.delay(520),
      Animated.timing(cover, { toValue: 0, duration: 260, useNativeDriver: true }),
    ]);
    run.start(({ finished }) => {
      if (finished) setDone(true);
    });
    return () => run.stop();
  }, [word, cover]);

  if (done) return null;
  return (
    <Animated.View
      pointerEvents="none"
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={[StyleSheet.absoluteFill, styles.center, { backgroundColor: palettes[scheme].bg, opacity: cover }]}
      testID="launch-cover"
    >
      {/* The icon stays at the screen's centre, where the splash drew it;
          the wordmark hangs below it. */}
      <View style={styles.icon}>
        <Animated.View style={{ transform: [{ scale: word.interpolate({ inputRange: [0, 1], outputRange: [start, 1] }) }] }} testID="launch-mark-grow">
          <BrandMark size={LAUNCH_ICON} testID="launch-mark" />
        </Animated.View>
        <Animated.View
          style={[styles.word, { opacity: word, transform: [{ translateY: word.interpolate({ inputRange: [0, 1], outputRange: [8, 0] }) }] }]}
        >
          <Wordmark size={36} scheme={scheme} testID={`launch-wordmark-${scheme}`} />
        </Animated.View>
      </View>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  center: { alignItems: 'center', justifyContent: 'center' },
  icon: { width: LAUNCH_ICON, height: LAUNCH_ICON, alignItems: 'center' },
  word: { position: 'absolute', top: LAUNCH_ICON + 24, left: (LAUNCH_ICON - 400) / 2, width: 400, alignItems: 'center' },
});
