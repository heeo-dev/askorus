// The launch cover (#1): the native splash can draw only the icon (Android
// 12+), so the app opens on the same icon in the same place, in the same
// light or dark as the splash, and the wordmark comes up under it as on
// the board F-Brand-Splash-Final; then the cover fades to the first screen.
// It shows once per start, never takes a touch and is hidden from screen
// readers (the first screen is what they should land on). Where the splash
// drew the icon smaller, the cover's icon starts at that size and grows as
// the wordmark comes up (R-63). The splash stays until the cover's icon is
// drawn: the icon is an image that comes a moment after the cover's first
// frame, and the screen showed no icon in between (R-67).

import * as SplashScreen from 'expo-splash-screen';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Animated, Platform, StyleSheet, useColorScheme, View } from 'react-native';

import { palettes, resolveScheme } from './theme';
import { BrandMark, Wordmark } from './Wordmark';

/** The icon's size, as app.json gives the native splash (imageWidth). */
export const LAUNCH_ICON = 112;

/** How long the splash waits for the cover's icon at most (ms). */
export const SPLASH_WAIT = 1500;

SplashScreen.preventAutoHideAsync().catch(() => undefined);

/**
 * splashIconScale is how big the native splash drew the icon, as a share of
 * LAUNCH_ICON. Android 12+ draws imageWidth on a 288 dp canvas: stock
 * Android (and the splash library before 12) shows the canvas at 288 dp,
 * Samsung's One UI on Android 12 at half that (the S10, 11-03). Samsung on
 * Android 13 and later shows it whole: on the S23 a cover icon starting at
 * half grew behind the splash's full one (R-69). iOS draws imageWidth points.
 */
export function splashIconScale(d: { os: string; version: number | string; manufacturer?: string }): number {
  const v = Number(d.version);
  return d.os === 'android' && v >= 31 && v <= 32 && /samsung/i.test(d.manufacturer ?? '') ? 0.5 : 1;
}

function deviceSplashIconScale(): number {
  const manufacturer = Platform.OS === 'android' ? Platform.constants.Manufacturer : undefined;
  return splashIconScale({ os: Platform.OS, version: Platform.Version, manufacturer });
}

export function LaunchCover({ onDone }: { onDone?: () => void } = {}) {
  const scheme = resolveScheme('system', useColorScheme());
  const [done, setDone] = useState(false);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  useEffect(() => {
    if (done) doneRef.current?.();
  }, [done]);
  const word = useRef(new Animated.Value(0)).current;
  const cover = useRef(new Animated.Value(1)).current;
  const start = useRef(deviceSplashIconScale()).current;
  // The icon is drawn (or the wait ran out): the splash goes and the cover moves.
  const [ready, setReady] = useState(false);
  const iconDrawn = useCallback(() => setReady(true), []);

  useEffect(() => {
    const t = setTimeout(iconDrawn, SPLASH_WAIT);
    return () => clearTimeout(t);
  }, [iconDrawn]);

  useEffect(() => {
    if (!ready) return;
    // The next frame has the icon on it. Not cancelled: once the icon is
    // there the splash must go, whatever happens to the cover.
    requestAnimationFrame(() => SplashScreen.hide());
    const run = Animated.sequence([
      Animated.timing(word, { toValue: 1, duration: 280, delay: 80, useNativeDriver: true }),
      Animated.delay(520),
      Animated.timing(cover, { toValue: 0, duration: 260, useNativeDriver: true }),
    ]);
    run.start(({ finished }) => {
      if (finished) setDone(true);
    });
    return () => run.stop();
  }, [ready, word, cover]);

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
          <BrandMark size={LAUNCH_ICON} testID="launch-mark" onLoad={iconDrawn} />
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
