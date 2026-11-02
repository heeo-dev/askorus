// The two entrances (extension principle 9, 2026-10-29): 한 번에 / All at
// once ('/', ask, and work in Phase 4) and 내 PC / My PC ('/pcs', remote
// sessions). Since S5c the app opens on 한 번에 and the switch between them
// is in each entrance's header (ui/Header), with the drawer beside it
// (ShellProvider), so the tab bar is hidden. Both tabs stay mounted, so a
// conversation survives a trip to 내 PC, and Android's back goes to the
// entrance that came before. expo-router 58 shows only the tabs a layout
// declares, so both are named here.

import { Tabs } from 'expo-router';

import { ShellProvider } from '@/ui/shell/Shell';
import { useTheme } from '@/ui/theme';

export default function TabsLayout() {
  const th = useTheme();
  return (
    <ShellProvider>
      <Tabs tabBar={() => null} backBehavior="history" screenOptions={{ headerShown: false, sceneStyle: { backgroundColor: th.bg } }}>
        <Tabs.Screen name="index" />
        <Tabs.Screen name="pcs" />
      </Tabs>
    </ShellProvider>
  );
}
