import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';

import { t } from '@/i18n';
import { useTheme } from '@/ui/theme';

// The two entrances are tabs (extension principle 9, see (tabs)/_layout);
// the screens they open stack on top of them.
export default function RootLayout() {
  const th = useTheme();
  return (
    <>
      <StatusBar style="auto" />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: th.card },
          headerTintColor: th.text,
          contentStyle: { backgroundColor: th.bg },
        }}
      >
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="pair" options={{ title: t('pair.title') }} />
        <Stack.Screen name="settings" options={{ title: t('settings.title') }} />
        <Stack.Screen name="pc/[id]/index" options={{ title: '' }} />
        <Stack.Screen name="pc/[id]/new" options={{ title: t('new.title') }} />
        <Stack.Screen name="pc/[id]/folders" options={{ title: t('folders.title') }} />
      </Stack>
    </>
  );
}
