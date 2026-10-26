import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';

import { t } from '@/i18n';
import { useTheme } from '@/ui/theme';

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
        <Stack.Screen name="index" options={{ title: t('home.title') }} />
        <Stack.Screen name="pair" options={{ title: t('pair.title') }} />
        <Stack.Screen name="pc/[id]" options={{ title: '' }} />
      </Stack>
    </>
  );
}
