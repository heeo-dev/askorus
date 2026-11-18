// The top bar's usage button and its sheet (#20, decisions 15-21, agreed by
// 1호 and 2호 on 2026-11-18): a bar per AI turned on, in the composer's
// order, as high as the AI's tightest window has left. Under 20% the bar is
// a short orange fill; a window used up is an empty bar with a line across
// its middle; an AI that reports nothing is a dashed bar (never 0%). The
// first read shows '…', no AI turned on '−'. A press opens 'Usage left':
// a summary line, each AI's windows with what is left and when they reset,
// Claude's 'as of', why an AI has none, the AIs turned off, and the way to
// Manage AIs. The sheet reads the PC again when it opens.

import { router, useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs } from '@/ask/prefs';
import { whenAgain } from '@/ask/present';
import { type AIUsage, lowest, percentLeft, stateOf, summarize, type UsageGroup, type UsageSummary } from '@/ask/usage';
import { t } from '@/i18n';

import { Avatar } from './Avatar';
import { Button } from './Button';
import { refreshPCUsage, refreshStatusThenUsage, usePCUsage } from './shell/pcUsage';
import { usePCs } from './shell/pcs';
import { Sheet } from './Sheet';
import { themed, type Theme, useTheme } from './theme';

const windowOrder = ['5h', 'week', 'month'];

/** windowLabel names a window ('5시간', '이번 주', '이번 달'), else the AI's own name for the group. */
export function windowLabel(g: UsageGroup): string {
  return g.window && windowOrder.includes(g.window) ? t(`usage.window.${g.window}` as 'usage.window.5h') : g.name;
}

/** limitName names a window's limit in a sentence ('5시간', '주간', '월간'). */
function limitName(g: UsageGroup): string {
  return g.window && windowOrder.includes(g.window) ? t(`usage.limit.${g.window}` as 'usage.limit.5h') : g.name;
}

/** summaryLine is the sheet's first line: a used-up AI, else the lowest under 20%, else all fine. */
export function summaryLine(sum: UsageSummary): string {
  if (sum.kind === 'loading') return t('usage.sum.loading');
  if (sum.kind === 'off') return t('usage.allOff');
  const low = lowest(sum.ais);
  if (!low?.tight) return t('usage.sum.unknown');
  const name = agentName(low.agent);
  switch (stateOf(low.tight)) {
    case 'out':
      return t('usage.sum.out', { name, window: limitName(low.tight) });
    case 'low':
      return t('usage.sum.low', { name, window: limitName(low.tight), pct: percentLeft(low.tight.left) });
  }
  // R-143 (2ho's review): an AI that reports nothing is not 'more than 20% left'.
  const unknown = sum.ais.filter((a) => !a.tight).map((a) => agentName(a.agent));
  return unknown.length ? t('usage.sum.okSome', { names: unknown.join(', ') }) : t('usage.sum.ok');
}

/** a11yValue reads the button's state: the summary line, then each AI's bar. */
export function a11yValue(sum: UsageSummary): string {
  if (sum.kind !== 'ready') return summaryLine(sum);
  const each = sum.ais.map((a) => {
    const name = agentName(a.agent);
    if (!a.tight) return `${name} ${t('usage.unknownShort')}`;
    return a.state === 'out' ? `${name} ${t('usage.out')}` : `${name} ${percentLeft(a.tight.left)}%`;
  });
  const head = summaryLine(sum);
  return `${head}${/[.!?]$/.test(head) ? '' : '.'} ${each.join(', ')}`;
}

/** The icon of the button: the bars, or '…' / '−'. */
export function UsageBars({ sum, th }: { sum: UsageSummary; th: Theme }) {
  const s = styles(th);
  if (sum.kind === 'loading') return <Text style={s.mark}>…</Text>;
  if (sum.kind === 'off') return <Text style={s.mark}>−</Text>;
  return (
    <View style={s.bars} testID="usage-bars">
      {sum.ais.slice(0, 4).map((a) => (
        <Bar key={a.agent} a={a} th={th} />
      ))}
    </View>
  );
}

function Bar({ a, th }: { a: AIUsage; th: Theme }) {
  const s = styles(th);
  if (!a.tight) return <View style={[s.bar, s.unknown]} testID={`usage-bar-${a.agent}-unknown`} />;
  if (a.state === 'out') {
    return (
      <View style={[s.bar, s.out]} testID={`usage-bar-${a.agent}-out`}>
        <View style={s.cross} />
      </View>
    );
  }
  const h = Math.max(3, Math.round(Math.min(1, a.tight.left) * 18));
  return (
    <View style={[s.bar, s.barTrack]} testID={`usage-bar-${a.agent}-${a.state}`}>
      <View style={[s.fill, { height: h }, a.state === 'low' ? s.lowFill : s.okFill]} />
    </View>
  );
}

/**
 * UsageMini is a usage line under a row (Manage AIs): a short bar and the
 * tightest window's words, orange when low and red when used up.
 */
export function UsageMini({ g }: { g: UsageGroup }) {
  const th = useTheme();
  const s = styles(th);
  const st = stateOf(g);
  const pct = percentLeft(g.left);
  const window = windowLabel(g);
  return (
    <View style={s.mini}>
      <View style={s.miniTrack}>
        {st === 'out' ? null : <View style={[s.miniFill, { width: `${pct}%` }, st === 'low' && s.lowFill]} />}
      </View>
      <Text style={[s.miniText, st === 'low' && s.lowText, st === 'out' && s.outText]}>
        {st === 'out' ? t('usage.windowOut', { window }) : t('usage.windowLeft', { window, pct })}
      </Text>
    </View>
  );
}

/** usageWords is UsageMini's words, for a row's label. */
export function usageWords(g: UsageGroup): string {
  const window = windowLabel(g);
  return stateOf(g) === 'out' ? t('usage.windowOut', { window }) : t('usage.windowLeft', { window, pct: percentLeft(g.left) });
}

/** usePrefs reads the ask preferences each time the screen comes into focus (Manage AIs may change them). */
function usePrefs(): AskPrefs {
  const [prefs, setPrefs] = useState<AskPrefs>(defaultAskPrefs);
  useFocusEffect(
    useCallback(() => {
      let live = true;
      loadAskPrefs()
        .then((p) => live && setPrefs(p))
        .catch(() => undefined);
      return () => {
        live = false;
      };
    }, []),
  );
  return prefs;
}

/** UsageButton is the top bar's button; null without a paired PC. */
export function UsageButton() {
  const th = useTheme();
  const s = styles(th);
  const u = usePCUsage();
  const { current } = usePCs();
  const prefs = usePrefs();
  const [open, setOpen] = useState(false);
  // A screen opened before anything read the PC's info (a stacked screen
  // the app starts on): read it, after the status.
  const has = !!u;
  useEffect(() => {
    if (current && !has) refreshStatusThenUsage().catch(() => undefined);
  }, [current, has]);
  if (!current) return <View style={s.slot} />;
  const sum = summarize(u?.info ?? null, prefs.off);
  return (
    <>
      <Pressable
        onPress={() => {
          setOpen(true);
          refreshPCUsage({ force: true }).catch(() => undefined);
        }}
        accessibilityRole="button"
        accessibilityLabel={t('usage.button')}
        accessibilityValue={{ text: a11yValue(sum) }}
        style={s.slot}
        testID="usage-button"
      >
        <UsageBars sum={sum} th={th} />
      </Pressable>
      <Sheet open={open} onClose={() => setOpen(false)} title={t('usage.title')} subtitle={t('usage.pc', { pc: current.name })} scroll testID="usage-sheet">
        <UsageSheetBody sum={sum} staleAt={u?.failed ? u.at : undefined} onManage={() => setOpen(false)} />
      </Sheet>
    </>
  );
}

function UsageSheetBody({ sum, staleAt, onManage }: { sum: UsageSummary; staleAt?: number; onManage: () => void }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.body}>
      <Text style={s.sum}>{summaryLine(sum)}</Text>
      {staleAt !== undefined ? <Text style={s.note}>{t('usage.stale', { time: whenAgain(new Date(staleAt).toISOString()) })}</Text> : null}
      {sum.kind === 'ready'
        ? sum.ais.map((a) => (
            <View key={a.agent} style={s.ai} testID={`usage-ai-${a.agent}`}>
              <View style={s.aiHead}>
                <Avatar agent={a.agent} size={24} />
                <Text style={s.aiName}>{agentName(a.agent)}</Text>
              </View>
              {a.groups.length === 0 ? <Text style={s.note}>{t(a.agent === 'claude' || a.agent === 'opencode' ? (`usage.none.${a.agent}` as 'usage.none.claude') : 'usage.none')}</Text> : null}
              {[...a.groups]
                .sort((x, y) => windowOrder.indexOf(x.window ?? '') - windowOrder.indexOf(y.window ?? ''))
                .map((g) => (
                  <WindowRow key={`${g.window ?? ''}${g.name}`} g={g} th={th} />
                ))}
              {a.groups[0]?.at ? <Text style={s.note}>{t('usage.asOf', { time: whenAgain(a.groups[0].at) })}</Text> : null}
            </View>
          ))
        : null}
      {sum.kind !== 'loading' && sum.off.length > 0 ? <Text style={s.note}>{t('usage.off', { names: sum.off.map(agentName).join(', ') })}</Text> : null}
      <Button
        label={t('usage.manage')}
        secondary
        onPress={() => {
          onManage();
          router.push('/ais');
        }}
      />
    </View>
  );
}

function WindowRow({ g, th }: { g: UsageGroup; th: Theme }) {
  const s = styles(th);
  const st = stateOf(g);
  const pct = percentLeft(g.left);
  return (
    <View style={s.win}>
      <View style={s.winHead}>
        <Text style={s.winName}>{windowLabel(g)}</Text>
        <Text style={[s.winPct, st === 'low' && s.lowText, st === 'out' && s.outText]}>{st === 'out' ? t('usage.out') : t('usage.left', { pct })}</Text>
      </View>
      <View style={s.track}>
        <View style={[s.trackFill, { width: `${st === 'out' ? 0 : pct}%` }, st === 'low' && s.lowFill]} />
      </View>
      {g.resetAt ? <Text style={s.reset}>{t('ais.resets', { time: whenAgain(g.resetAt) })}</Text> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    slot: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
    mark: { fontSize: 16, fontWeight: '600', color: th.textSub },
    bars: { flexDirection: 'row', alignItems: 'flex-end', gap: 3, height: 18 },
    bar: { width: 5, height: 18, borderRadius: 2.5, overflow: 'hidden', justifyContent: 'flex-end' },
    barTrack: { backgroundColor: th.borderStrong },
    okFill: { backgroundColor: th.textSub },
    lowFill: { backgroundColor: th.mark },
    fill: { width: '100%' },
    out: { borderWidth: 1.5, borderColor: th.danger, justifyContent: 'center' },
    cross: { height: 1.5, backgroundColor: th.danger },
    unknown: { borderWidth: 1.2, borderColor: th.caption, borderStyle: 'dashed' },
    mini: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 4 },
    miniTrack: { width: 44, height: 4, borderRadius: 2, backgroundColor: th.borderStrong, overflow: 'hidden' },
    miniFill: { height: '100%', borderRadius: 2, backgroundColor: th.textSub },
    miniText: { fontSize: 13, lineHeight: 18, color: th.textSub },
    body: { gap: 12, paddingHorizontal: 16, paddingBottom: 12 },
    sum: { fontSize: 14, lineHeight: 20, color: th.text, backgroundColor: th.bg, borderRadius: 10, paddingHorizontal: 12, paddingVertical: 9 },
    ai: { gap: 6, paddingBottom: 10, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: th.divider },
    aiHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
    aiName: { fontSize: 15, fontWeight: '600', color: th.text },
    win: { gap: 3 },
    winHead: { flexDirection: 'row', justifyContent: 'space-between' },
    winName: { fontSize: 13, color: th.textSub },
    winPct: { fontSize: 13, fontWeight: '500', color: th.text, fontVariant: ['tabular-nums'] },
    lowText: { color: th.mark },
    outText: { color: th.danger },
    track: { height: 5, borderRadius: 3, backgroundColor: th.segLine, overflow: 'hidden' },
    trackFill: { height: '100%', borderRadius: 3, backgroundColor: th.textSub },
    reset: { fontSize: 12, color: th.caption },
    note: { fontSize: 13, lineHeight: 19, color: th.caption },
  }),
);
