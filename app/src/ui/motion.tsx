// Small motions for live screens (#36, the debate): a block that rises in
// when it arrives, three dots while an AI is answering, and a dot that
// breathes while something runs. They move only opacity and transform
// (native driver), only while the screen is live, and never when the
// phone asks for less motion (Settings > Accessibility > Remove
// animations): then each shows its still, final look.

import { type ReactNode, useEffect, useRef, useSyncExternalStore } from 'react';
import { AccessibilityInfo, Animated, StyleSheet, View } from 'react-native';

import { Spinner } from './controls';
import { useTheme } from './theme';

// The phone's 'less motion' setting, read once when first needed and kept
// up to date: still (true) until it is known.
let reduceNow = true;
const watchers = new Set<() => void>();
const tell = (on: boolean) => {
  reduceNow = on;
  watchers.forEach((w) => w());
};
let read = false;
function watchMotion(w: () => void): () => void {
  if (!read) {
    read = true;
    AccessibilityInfo.addEventListener('reduceMotionChanged', tell);
    AccessibilityInfo.isReduceMotionEnabled()
      .then(tell)
      .catch(() => undefined); // keep the still look
  }
  watchers.add(w);
  return () => void watchers.delete(w);
}

/** useReduceMotion is the phone's 'less motion' setting (true until read). */
export function useReduceMotion(): boolean {
  return useSyncExternalStore(watchMotion, () => reduceNow);
}

/**
 * Appear rises its children in once, when they first show while live
 * (a new turn, an outcome). Opened from history, or with less motion,
 * they are simply there. Whether to move is settled when it first shows,
 * so nothing already on screen rises again later.
 */
export function Appear({ live, children, testID }: { live: boolean; children: ReactNode; testID?: string }) {
  const reduce = useReduceMotion();
  const play = useRef(live && !reduce).current;
  const v = useRef(new Animated.Value(play ? 0 : 1)).current;
  useEffect(() => {
    if (!play) return;
    const run = Animated.timing(v, { toValue: 1, duration: 240, useNativeDriver: true, isInteraction: false });
    run.start();
    return () => run.stop();
  }, [play, v]);
  return (
    <Animated.View testID={testID} style={{ opacity: v, transform: [{ translateY: v.interpolate({ inputRange: [0, 1], outputRange: [8, 0] }) }] }}>
      {children}
    </Animated.View>
  );
}

/** Dots are three dots lighting up in turn while an AI is answering. */
export function Dots() {
  const th = useTheme();
  const reduce = useReduceMotion();
  const values = useRef([0, 1, 2].map(() => new Animated.Value(0.35))).current;
  useEffect(() => {
    if (reduce) {
      values.forEach((v) => v.setValue(0.6));
      return;
    }
    const one = (v: Animated.Value) =>
      Animated.sequence([
        Animated.timing(v, { toValue: 1, duration: 300, useNativeDriver: true, isInteraction: false }),
        Animated.timing(v, { toValue: 0.35, duration: 300, useNativeDriver: true, isInteraction: false }),
      ]);
    const run = Animated.loop(Animated.stagger(160, values.map(one)));
    run.start();
    return () => run.stop();
  }, [reduce, values]);
  return (
    <View style={dots.row} accessibilityElementsHidden importantForAccessibility="no-hide-descendants" testID="dots">
      {values.map((v, i) => (
        <Animated.View key={i} style={[dots.dot, { backgroundColor: th.mark, opacity: v }]} />
      ))}
    </View>
  );
}

const dots = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 5, height: 20 },
  dot: { width: 6, height: 6, borderRadius: 3 },
});

/**
 * Working is the round busy mark, or with less motion a still dot in its
 * place (the spinner would keep turning otherwise).
 */
export function Working({ size = 16 }: { size?: number }) {
  const th = useTheme();
  const reduce = useReduceMotion();
  if (!reduce) return <Spinner size={size} />;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }} testID="working-still">
      <View style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: th.mark }} />
    </View>
  );
}

/** LiveDot breathes while something runs (next to the debate's title). */
export function LiveDot() {
  const th = useTheme();
  const reduce = useReduceMotion();
  const v = useRef(new Animated.Value(1)).current;
  useEffect(() => {
    if (reduce) {
      v.setValue(1);
      return;
    }
    const run = Animated.loop(
      Animated.sequence([
        Animated.timing(v, { toValue: 0.3, duration: 700, useNativeDriver: true, isInteraction: false }),
        Animated.timing(v, { toValue: 1, duration: 700, useNativeDriver: true, isInteraction: false }),
      ]),
    );
    run.start();
    return () => run.stop();
  }, [reduce, v]);
  return (
    <Animated.View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      testID="live-dot"
      style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: th.mark, opacity: v }}
    />
  );
}
