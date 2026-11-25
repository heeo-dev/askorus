// The phone's clock for a view that counts seconds while something runs
// (#73). The polls used to redraw the Ask screen every second, and the
// waiting seconds moved with them; a run that has not changed is no longer
// handed on, so a view that counts keeps its own clock while it runs.

import { useEffect, useState } from 'react';

/**
 * useNow returns the later of given (the clock of the parent's last draw)
 * and its own clock, which ticks every everyMs while active. Inactive, it is
 * given and nothing ticks.
 */
export function useNow(active: boolean, given: number, everyMs = 1000): number {
  const [tick, setTick] = useState(given);
  useEffect(() => {
    if (!active) return;
    setTick(Date.now());
    const id = setInterval(() => setTick(Date.now()), everyMs);
    return () => clearInterval(id);
  }, [active, everyMs]);
  return active ? Math.max(given, tick) : given;
}
