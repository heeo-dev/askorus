// The two entrances (extension principle 9, 2026-10-29): 한 번에 / All at
// once (ask, and work in Phase 4) and 내 PC / My PCs (remote sessions).
// The app still opens on My PCs (the index route, "/"), as before Phase 2.

import { router, Tabs } from 'expo-router';
import { type ColorValue, Pressable, Text } from 'react-native';

import { t } from '@/i18n';
import { useTheme } from '@/ui/theme';

export default function TabsLayout() {
  const th = useTheme();
  const settings = () => (
    <Pressable onPress={() => router.push('/settings')} accessibilityRole="button" hitSlop={8} style={{ marginRight: 16 }}>
      <Text style={{ color: th.accent, fontSize: 16 }}>{t('home.settings')}</Text>
    </Pressable>
  );
  // A small mark above each label; without one the tab bar draws a placeholder.
  const mark = (c: string) =>
    function Mark({ color }: { color: ColorValue }) {
      return <Text style={{ color, fontSize: 18 }}>{c}</Text>;
    };
  return (
    <Tabs
      screenOptions={{
        headerStyle: { backgroundColor: th.card },
        headerTintColor: th.text,
        headerRight: settings,
        tabBarStyle: { backgroundColor: th.card, borderTopColor: th.border },
        tabBarHideOnKeyboard: true, // it rode above the keyboard on the S10 (P2-2)
        tabBarActiveTintColor: th.accent,
        tabBarInactiveTintColor: th.muted,
        sceneStyle: { backgroundColor: th.bg },
      }}
    >
      <Tabs.Screen name="ask" options={{ title: t('ask.title'), tabBarLabel: t('tab.ask'), tabBarIcon: mark('✦') }} />
      <Tabs.Screen name="index" options={{ title: t('home.title'), tabBarLabel: t('tab.pcs'), tabBarIcon: mark('▣') }} />
    </Tabs>
  );
}
