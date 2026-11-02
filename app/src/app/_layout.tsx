import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { StyleSheet, useColorScheme, View } from 'react-native';

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
    <>
      <StatusBar style={th.scheme === 'dark' ? 'light' : 'dark'} />
      <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: th.bg } }} />
      {/* Until the saved theme is read, the phone's background covers the
          screens, so a dark choice never flashes light (the stack stays mounted). */}
      {ready ? null : <View style={[StyleSheet.absoluteFill, { backgroundColor: palettes[resolveScheme('system', system)].bg }]} />}
    </>
  );
}
