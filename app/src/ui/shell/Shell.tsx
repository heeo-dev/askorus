// The frame around the two entrances (S5c): the drawer, opened from the
// header's menu button, and the reading of the current PC's status
// (pcStatus.ts) while the app is in front.

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { AppState } from 'react-native';

import { Drawer } from '../Drawer';
import { usePCs } from './pcs';
import { refreshPCStatus, statusEveryMs } from './pcStatus';

type ShellCtx = { openDrawer: () => void; closeDrawer: () => void; drawerOpen: boolean };

const none: ShellCtx = { openDrawer: () => undefined, closeDrawer: () => undefined, drawerOpen: false };
const ShellContext = createContext<ShellCtx>(none);

export function ShellProvider({ children }: { children: ReactNode }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const currentId = usePCs().current?.id;

  // On start, on a new current PC, when the app comes to the front, and
  // every 30 s while it is in front.
  useEffect(() => {
    refreshPCStatus().catch(() => undefined);
  }, [currentId]);
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active') refreshPCStatus().catch(() => undefined);
    });
    const timer = setInterval(() => {
      if (AppState.currentState === 'active') refreshPCStatus().catch(() => undefined);
    }, statusEveryMs);
    return () => {
      sub.remove();
      clearInterval(timer);
    };
  }, []);

  const openDrawer = useCallback(() => {
    setDrawerOpen(true);
    refreshPCStatus().catch(() => undefined);
  }, []);
  const closeDrawer = useCallback(() => setDrawerOpen(false), []);
  const value = useMemo(() => ({ openDrawer, closeDrawer, drawerOpen }), [openDrawer, closeDrawer, drawerOpen]);

  return (
    <ShellContext.Provider value={value}>
      {children}
      <Drawer open={drawerOpen} onClose={closeDrawer} />
    </ShellContext.Provider>
  );
}

/** useShell opens and closes the drawer; outside a ShellProvider it does nothing. */
export function useShell(): ShellCtx {
  return useContext(ShellContext);
}
