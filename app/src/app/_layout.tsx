import { router, Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { Pressable, Text, View } from 'react-native';

import { features } from '@/features';
import { t } from '@/i18n';
import { useTheme } from '@/ui/theme';

export default function RootLayout() {
  const th = useTheme();
  const link = (label: string, to: '/settings' | '/ask') => (
    <Pressable onPress={() => router.push(to)} accessibilityRole="button" hitSlop={8}>
      <Text style={{ color: th.accent, fontSize: 16 }}>{label}</Text>
    </Pressable>
  );
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
              <View style={{ flexDirection: 'row', gap: 16 }}>
                {features.ask ? link(t('ask.title'), '/ask') : null}
                {link(t('home.settings'), '/settings')}
              </View>
            ),
          }}
        />
        <Stack.Screen name="pair" options={{ title: t('pair.title') }} />
        <Stack.Screen name="settings" options={{ title: t('settings.title') }} />
        <Stack.Screen name="ask" options={{ title: t('ask.title') }} />
        <Stack.Screen name="pc/[id]/index" options={{ title: '' }} />
        <Stack.Screen name="pc/[id]/new" options={{ title: t('new.title') }} />
        <Stack.Screen name="pc/[id]/folders" options={{ title: t('folders.title') }} />
      </Stack>
    </>
  );
}
