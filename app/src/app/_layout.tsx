import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { StyleSheet, useColorScheme, View } from 'react-native';

import { conversations } from '@/ask/conversations';
import { resumeTailnet } from '@/tailnet/node';

import { setRootBottom } from '@/ui/keyboard';
import { LaunchCover } from '@/ui/LaunchCover';
import { applyNavigationBar } from '@/ui/navigationBar';
import { palettes, resolveScheme, ThemeProvider, useTheme, useThemeChoice } from '@/ui/theme';

// The two entrances are tabs (extension principle 9, see (tabs)/_layout);
// the screens they open stack on top of them. Every screen draws its own
// top (Header or TopBar, S5c), so the stack shows no header. The kept
// conversations' heads are read once, for the drawer (milestone B). The
// app's Tailscale node starts here when the person signed in before (#57),
// so the first request to a PC does not wait for it.
export default function RootLayout() {
  useEffect(() => {
    conversations.init();
    void resumeTailnet();
  }, []);
  return (
    <ThemeProvider>
      <Root />
    </ThemeProvider>
  );
}

function Root() {
  const th = useTheme();
  const { ready } = useThemeChoice();
  const system = useColorScheme();
  // The navigation bar follows the theme (#17); again once the launch
  // cover is done, as the splash put it back the way it was at start-up.
  const [launched, setLaunched] = useState(false);
  useEffect(() => {
    applyNavigationBar(th.scheme);
  }, [th.scheme, launched]);
  return (
    // Where the app ends on the screen, for the keyboard's lift (R-62).
    <View style={styles.root} onLayout={(e) => setRootBottom(e.nativeEvent.layout.y + e.nativeEvent.layout.height)}>
      <StatusBar style={th.scheme === 'dark' ? 'light' : 'dark'} />
      <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: th.bg } }} />
      {/* Until the saved theme is read, the phone's background covers the
          screens, so a dark choice never flashes light (the stack stays mounted). */}
      {ready ? null : <View style={[StyleSheet.absoluteFill, { backgroundColor: palettes[resolveScheme('system', system)].bg }]} />}
      <LaunchCover onDone={() => setLaunched(true)} />
    </View>
  );
}

const styles = StyleSheet.create({ root: { flex: 1 } });
