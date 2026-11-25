import { act, renderHook } from '@testing-library/react-native';

import { useNow } from './useNow';

// #73 (2호's review): the Ask screen is no longer redrawn every second
// while the AIs answer, so a turn that runs counts its seconds by itself,
// and stops when it no longer runs.
test('the clock moves while active and stops when it is not', async () => {
  jest.useFakeTimers({ now: 1_000_000 });
  try {
    const { result, rerender } = await renderHook(({ active, given }: { active: boolean; given: number }) => useNow(active, given), {
      initialProps: { active: true, given: 1_000_000 },
    });
    await act(async () => {
      jest.advanceTimersByTime(3000);
    });
    expect(result.current).toBeGreaterThanOrEqual(1_003_000);

    await rerender({ active: false, given: 1_000_500 });
    expect(result.current).toBe(1_000_500); // the parent's clock, nothing ticks
    await act(async () => {
      jest.advanceTimersByTime(3000);
    });
    expect(result.current).toBe(1_000_500);
  } finally {
    jest.useRealTimers();
  }
});
