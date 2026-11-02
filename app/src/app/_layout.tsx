import { NavigationBar } from 'expo-navigation-bar';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { StyleSheet, useColorScheme, View } from 'react-native';

import { setRootBottom } from '@/ui/keyboard';
import { LaunchCover } from '@/ui/LaunchCover';
import { palettes, resolveScheme, ThemeProvider, useTheme, useThemeChoice } from '@/ui/theme';

// The two entrances are tabs (extension principle 9, see (tabs)/_layout);
// the screens they open stack on top of them. Every screen draws its own
// top (Header or TopBar, S5c), so the stack shows no header.
export default function RootLayout() {
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
  return (
    // Where the app ends on the screen, for the keyboard's lift (R-62).
    <View style={styles.root} onLayout={(e) => setRootBottom(e.nativeEvent.layout.y + e.nativeEvent.layout.height)}>
      <StatusBar style={th.scheme === 'dark' ? 'light' : 'dark'} />
      {/* The navigation bar follows the theme too: Android sets it from the
          phone's light or dark only as the app starts, so a theme chosen in
          the app, or the phone's changed while the app runs, left it the
          other way (R-65). */}
      <NavigationBar style={th.scheme === 'dark' ? 'dark' : 'light'} />
      <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: th.bg } }} />
      {/* Until the saved theme is read, the phone's background covers the
          screens, so a dark choice never flashes light (the stack stays mounted). */}
      {ready ? null : <View style={[StyleSheet.absoluteFill, { backgroundColor: palettes[resolveScheme('system', system)].bg }]} />}
      <LaunchCover />
    </View>
  );
}

const styles = StyleSheet.create({ root: { flex: 1 } });
