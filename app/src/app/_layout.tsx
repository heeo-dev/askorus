import { router, Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { Pressable, Text } from 'react-native';

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
        <Stack.Screen
          name="index"
          options={{
            title: t('home.title'),
            headerRight: () => (
              <Pressable onPress={() => router.push('/settings')} accessibilityRole="button" hitSlop={8}>
                <Text style={{ color: th.accent, fontSize: 16 }}>{t('home.settings')}</Text>
              </Pressable>
            ),
          }}
        />
        <Stack.Screen name="pair" options={{ title: t('pair.title') }} />
        <Stack.Screen name="settings" options={{ title: t('settings.title') }} />
        <Stack.Screen name="pc/[id]/index" options={{ title: '' }} />
        <Stack.Screen name="pc/[id]/new" options={{ title: t('new.title') }} />
        <Stack.Screen name="pc/[id]/folders" options={{ title: t('folders.title') }} />
      </Stack>
    </>
  );
}
