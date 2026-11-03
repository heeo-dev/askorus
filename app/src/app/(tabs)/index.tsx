// 한 번에 / All at once ('/', S5c): the app opens here. One question goes to
// the current PC's AIs at once (POST /v1/runs); while they answer, who is
// at it; once the answers are in, the AIs judge each other's answers and
// one writes a combined answer, shown with where the AIs differ, the AIs'
// own answers as cards, and the evidence (F-Ask-Empty … F-Result-Light).
// Follow-ups go to everyone or one AI, each carrying its own conversation
// on; Claude's answer can be carried on in the Claude app.
//
// thread.ts follows the runs and comparisons; compare.ts and present.ts
// say what each state shows; ui/TurnView draws a turn. This screen keeps
// the conversation (in memory for this run of the app; the phone history
// is P2-6): 'New question' and opening another one from the drawer archive
// it in ask/conversations.ts. A conversation stays with the PC it was
// asked on.

import * as Clipboard from 'expo-clipboard';
import * as Crypto from 'expo-crypto';
import { router, useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Keyboard, Linking, Pressable, ScrollView, type ScrollViewInstance, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { AgentClient } from '@/api/client';
import { type Info, type RunPart, type Tier, tiers } from '@/api/types';
import { type CardKind, canSynthesize, judgedBy, polling, writerFor } from '@/ask/compare';
import { conversations, recentQuestions, useConversations } from '@/ask/conversations';
import { aiStatus, askErrorText, displayModel, metaLine, onlyTarget, partViews, rowValue, snapshotOf } from '@/ask/present';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { askers, type FollowDeps, followCompare, followRun, newTurn, running, stopAll, type Target, type Turn } from '@/ask/thread';
import { connect } from '@/connections/open';
import { type Cause, cause } from '@/connections/reach';
import { type Connection, ConnectionStore } from '@/connections/store';
import { errorText, has, t } from '@/i18n';
import { AISheet, type AISheetRow } from '@/ui/AISheet';
import { AnswerSheet } from '@/ui/AnswerSheet';
import { Button } from '@/ui/Button';
import { Composer, FollowBar } from '@/ui/Composer';
import { Spinner } from '@/ui/controls';
import { Header } from '@/ui/Header';
import { Icon, IconButton } from '@/ui/Icon';
import { JudgesSheet } from '@/ui/JudgesSheet';
import { ModelSheet } from '@/ui/ModelSheet';
import { PairSheet } from '@/ui/PairSheet';
import { ReachError } from '@/ui/ReachError';
import { usePCs } from '@/ui/shell/pcs';
import { themed, type Theme, useTheme } from '@/ui/theme';
import { TierSheet } from '@/ui/TierSheet';
import { type Note, type TurnActions, type TurnUI, TurnView } from '@/ui/TurnView';

const store = new ConnectionStore();

type PC = { conn: Connection; client: AgentClient };
type Load = { state: 'loading' } | { state: 'ok'; info: Info } | { state: 'error'; cause: Cause; text: string };
type Head = { id: string; pcId: string; startedAt: number; title: string };
// The one sheet open over the screen.
type SheetState =
  | { kind: 'pick' }
  | { kind: 'model'; agent: string }
  | { kind: 'tier' }
  | { kind: 'ais'; turn: string }
  | { kind: 'answer'; turn: string; agent: string }
  | { kind: 'pair'; turn: string; key: string }
  | { kind: 'judges'; turn: string }
  | null;

const noUI: TurnUI = { more: [], expanded: [] };

// An agent from before asks (P2-1) reports Claude alone and no tiers; a
// newer one always lists the AIs that only answer questions too.
function oldAgent(info: Info): boolean {
  return info.agents.every((a) => a.agent === 'claude' && !a.ask);
}

const judgeOn = (info: Info) => info.agents.some((a) => a.available && a.capabilities.includes('judge'));

export default function Ask() {
  const th = useTheme();
  const s = styles(th);
  const pcs = usePCs();
  const currentId = pcs.current?.id ?? null;
  const convs = useConversations();
  const [prefs, setPrefs] = useState<AskPrefs>(defaultAskPrefs);
  const [pc, setPC] = useState<PC | null>(null);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [turns, setTurns] = useState<Turn[]>([]);
  const [text, setText] = useState('');
  const [to, setTo] = useState<Target>('all');
  const [notes, setNotes] = useState<Record<string, Note>>({});
  const [ui, setUI] = useState<Record<string, TurnUI>>({});
  const [sheet, setSheet] = useState<SheetState>(null);
  const follows = useRef(new Map<string, AbortController>());
  const pcRef = useRef<PC | null>(null);
  const turnsRef = useRef<Turn[]>([]);
  const head = useRef<Head | null>(null);
  const saving = useRef<Promise<unknown>>(Promise.resolve());
  const scroll = useRef<ScrollViewInstance>(null);
  const stick = useRef(false);
  turnsRef.current = turns;

  // Prefs are saved one after another, each on what is stored, so a PC
  // picked on 내 PC is never written back over.
  const queue = useCallback((job: () => Promise<unknown>) => {
    saving.current = saving.current.then(job, job);
    return saving.current;
  }, []);
  const change = (patch: Partial<AskPrefs>) => {
    setPrefs((p) => ({ ...p, ...patch }));
    queue(async () => saveAskPrefs({ ...(await loadAskPrefs()), ...patch }));
  };

  // Back on this screen: Settings may have changed the tier, the
  // comparison or the combined answer, and the PCs may have changed.
  const reloadPCs = pcs.reload;
  useFocusEffect(
    useCallback(() => {
      queue(async () => setPrefs(await loadAskPrefs()));
      reloadPCs().catch(() => undefined);
    }, [queue, reloadPCs]),
  );

  const loadInfo = useCallback(async (p: PC) => {
    setLoad({ state: 'loading' });
    try {
      const info = await p.client.info();
      if (pcRef.current === p) setLoad({ state: 'ok', info });
    } catch (e) {
      if (pcRef.current === p) setLoad({ state: 'error', cause: cause(e), text: errorText(e) });
    }
  }, []);

  // What following a run or a comparison needs; thread.ts does the following.
  const deps: FollowDeps = useMemo(
    () => ({
      follows: follows.current,
      update: (id, patch) => setTurns((ts) => ts.map((x) => (x.id === id ? { ...x, ...patch(x) } : x))),
      newId: () => Crypto.randomUUID(),
      errorText: askErrorText,
    }),
    [],
  );

  // 'New question': stop following everything (the PC is asked to stop the
  // comparisons still running) and keep the conversation in this run's history.
  const fresh = useCallback(() => {
    stopAll(pcRef.current?.client, turnsRef.current, follows.current);
    const h = head.current;
    if (h && turnsRef.current.length) conversations.save({ ...h, turns: turnsRef.current });
    head.current = null;
    conversations.setCurrent(null);
    turnsRef.current = [];
    setTurns([]);
    setNotes({});
    setUI({});
    setSheet(null);
    setTo('all');
  }, []);

  // The current PC answers; its info says which AIs can.
  useEffect(() => {
    if (!currentId) {
      pcRef.current = null;
      setPC(null);
      return;
    }
    if (pcRef.current?.conn.id === currentId) return;
    let live = true;
    setLoad({ state: 'loading' });
    connect(store, currentId).then((p) => {
      if (!live) return;
      pcRef.current = p;
      setPC(p);
      if (p) loadInfo(p);
    });
    return () => {
      live = false;
    };
  }, [currentId, loadInfo]);

  // A conversation stays with its PC: another PC starts a new one.
  useEffect(() => {
    const h = head.current;
    if (currentId && h && h.pcId !== currentId && turnsRef.current.length) fresh();
  }, [currentId, fresh]);

  // A conversation opened from the drawer: the current one is kept, the
  // other comes back, and what was still running is followed again (an
  // unanswered run or comparison request goes again with the same id, so
  // the PC does it once), so nothing spins for ever.
  const reopen = useCallback(
    async (id: string) => {
      const c = conversations.get(id);
      if (!c) return;
      fresh();
      head.current = { id: c.id, pcId: c.pcId, startedAt: c.startedAt, title: c.title };
      conversations.setCurrent({ id: c.id, startedAt: c.startedAt, title: c.title });
      turnsRef.current = c.turns;
      setTurns(c.turns);
      if (c.pcId !== currentId && pcs.list?.some((x) => x.id === c.pcId)) await pcs.pick(c.pcId);
      const p = await connect(store, c.pcId);
      if (!p || head.current?.id !== c.id) return;
      for (const turn of c.turns) {
        if (running(turn)) {
          followRun(p.client, turn, deps, turn.run?.runId);
          continue;
        }
        const cmp = turn.compare;
        if (!cmp || turn.error || cmp.error) continue;
        if (cmp.key) followCompare(p.client, turn, 'start', deps);
        else if (cmp.id && (!cmp.snap || cmp.snap.comparisonId !== cmp.id || polling(cmp.snap))) followCompare(p.client, turn, 'poll', deps);
      }
    },
    [currentId, deps, fresh, pcs],
  );

  useEffect(() => {
    if (!convs.openRequest) return;
    const id = conversations.takeOpenRequest();
    if (id) reopen(id);
  }, [convs.openRequest, reopen]);

  useEffect(() => {
    const current = follows.current;
    return () => current.forEach((c) => c.abort());
  }, []);

  const info = load.state === 'ok' && pc ? load.info : null;
  const list = info ? askers(info) : [];
  const anyAsk = list.some((a) => a.canAsk);
  const on = list.filter((a) => a.canAsk && !prefs.off.includes(a.agent)).map((a) => a.agent);
  const busy = turns.some(running);
  const canOpen = info?.capabilities.includes('open_official') ?? false;
  const canAsk = (agent: string) => list.some((a) => a.agent === agent && a.canAsk);
  const now = Date.now();

  const send = () => {
    const question = text.trim();
    const p = pcRef.current;
    if (!p || !info || !question || busy) return;
    const turn = newTurn({
      id: Crypto.randomUUID(),
      question,
      tier: prefs.tier,
      to,
      on,
      turns,
      models: prefs.models,
      info,
      auto: prefs.compare,
      synth: { on: prefs.synth, agent: prefs.synthAgent },
      at: Date.now(),
    });
    if (!turn.agents.length) return;
    if (!head.current) {
      head.current = { id: Crypto.randomUUID(), pcId: p.conn.id, startedAt: Date.now(), title: question };
      conversations.setCurrent(head.current);
    }
    turnsRef.current = [...turnsRef.current, turn];
    setTurns((ts) => [...ts, turn]);
    setText('');
    setTo('all');
    Keyboard.dismiss();
    stick.current = true;
    followRun(p.client, turn, deps);
  };

  const note = (key: string, n: Note | null) =>
    setNotes((m) => {
      const next = { ...m };
      if (n) next[key] = n;
      else delete next[key];
      return next;
    });

  // Carries Claude's ask session on in the Claude app, with the same rules
  // as opening a session from 내 PC.
  const continueInClaude = async (key: string, session: string) => {
    const p = pcRef.current;
    if (!p) return;
    note(key, { text: t('ask.opening') });
    try {
      const r = await p.client.openSession(session);
      const hintKey = `sheet.hint.${r.hint}`;
      if (r.hint && has(hintKey)) {
        note(key, { text: t(hintKey) });
      } else if (r.remoteUrl) {
        note(key, null);
        await Linking.openURL(r.remoteUrl);
      } else {
        note(key, { text: errorText(new Error(r.action)), error: true });
      }
    } catch (e) {
      note(key, { text: askErrorText(e), error: true });
    }
  };

  const copy = async (key: string, answer: string) => {
    await Clipboard.setStringAsync(answer);
    note(key, { text: t('sheet.copied') });
  };

  const onlyOf = (agent: string, then?: () => void) => (canAsk(agent) ? () => (setTo({ agent }), then?.()) : undefined);
  const carryOn = (key: string, p: RunPart) =>
    p.agent === 'claude' && p.session && canOpen ? () => continueInClaude(key, p.session ?? '') : undefined;

  const setTurnUI = (id: string, f: (u: TurnUI) => TurnUI) => setUI((m) => ({ ...m, [id]: f(m[id] ?? noUI) }));

  const actsFor = (turn: Turn): TurnActions => ({
    compare: (how) => {
      const p = pcRef.current;
      if (!p) return;
      if (how === 'synth') followCompare(p.client, turn, how, deps, { agent: writerFor(info, prefs.synthAgent) });
      else followCompare(p.client, turn, how, deps);
    },
    retryRun: () => {
      const p = pcRef.current;
      if (p) followRun(p.client, turn, deps, turn.run?.runId);
    },
    openAnswer: (agent) => setSheet({ kind: 'answer', turn: turn.id, agent }),
    openPair: (key) => setSheet({ kind: 'pair', turn: turn.id, key }),
    openJudges: () => setSheet({ kind: 'judges', turn: turn.id }),
    openAIs: () => setSheet({ kind: 'ais', turn: turn.id }),
    copy,
    only: (agent) => onlyOf(agent),
    carryOn,
    setEvidence: (open) => setTurnUI(turn.id, (u) => ({ ...u, evidence: open })),
    more: (kind: CardKind) => setTurnUI(turn.id, (u) => ({ ...u, more: [...u.more, kind] })),
    toggleDiffer: (key) =>
      setTurnUI(turn.id, (u) => ({ ...u, expanded: u.expanded.includes(key) ? u.expanded.filter((k) => k !== key) : [...u.expanded, key] })),
  });

  // The last AI that is on stays on (UX-10): a question always has an AI to go to.
  const toggle = (agent: string) => {
    if (on.length === 1 && on[0] === agent) return;
    change({ off: prefs.off.includes(agent) ? prefs.off.filter((x) => x !== agent) : [...prefs.off, agent] });
  };

  // ---- The sheets ----

  const sheetTurn = sheet && 'turn' in sheet ? (turns.find((x) => x.id === sheet.turn) ?? null) : null;
  const sheetSnap = sheetTurn ? snapshotOf(sheetTurn) : undefined;
  const modelOf = (agent: string) => prefs.models[agent] || list.find((a) => a.agent === agent)?.tiers[prefs.tier]?.model || '';
  // Per tier, the models of the AIs that are on and follow the tier (#16).
  const tierModels = Object.fromEntries(
    tiers.map((x) => {
      const line = on
        .filter((a) => !prefs.models[a])
        .map((a) => {
          const m = list.find((l) => l.agent === a)?.tiers[x]?.model;
          return m ? `${agentName(a)} ${displayModel(m)}` : '';
        })
        .filter(Boolean)
        .join(' · ');
      return [x, line];
    }),
  ) as Record<Tier, string>;

  const pickRows: AISheetRow[] = list.map((a) => {
    const name = agentName(a.agent);
    return {
      agent: a.agent,
      name,
      model: a.canAsk ? displayModel(modelOf(a.agent)) || undefined : t('ask.error.agent_cannot_ask'),
      onPress: a.canAsk ? () => toggle(a.agent) : undefined,
      onLongPress: a.canAsk ? () => setSheet({ kind: 'model', agent: a.agent }) : undefined,
      disabled: !a.canAsk,
      checked: on.includes(a.agent),
      right: (
        <PickControls
          th={th}
          agent={a.agent}
          name={name}
          on={on.includes(a.agent)}
          disabled={!a.canAsk}
          onToggle={() => toggle(a.agent)}
          onModel={() => setSheet({ kind: 'model', agent: a.agent })}
        />
      ),
      accessibilityValue: { text: '' },
    };
  });

  const resultViews = sheet?.kind === 'ais' && sheetTurn ? partViews(sheetTurn, now, info) : [];
  const resultRows: AISheetRow[] = resultViews.map((p) => ({
    agent: p.agent,
    name: p.name,
    model: p.model || undefined,
    status: aiStatus(p),
    onPress: () => setSheet({ kind: 'answer', turn: sheetTurn?.id ?? '', agent: p.agent }),
    accessibilityValue: rowValue(p),
  }));
  const collecting = sheetTurn && running(sheetTurn) && sheetTurn.at !== undefined ? Math.max(0, Math.round((now - sheetTurn.at) / 1000)) : null;

  const answerView = sheet?.kind === 'answer' && sheetTurn ? (partViews(sheetTurn, now, info).find((p) => p.agent === sheet.agent) ?? null) : null;
  const answerKey = answerView && sheetTurn ? `${sheetTurn.id}/${answerView.agent}` : '';

  const sheets = (
    <>
      <AISheet
        open={sheet?.kind === 'pick'}
        onClose={() => setSheet(null)}
        title={t('ask.pickTitle')}
        subtitle={pc ? t('ask.onPC', { name: pc.conn.name }) : undefined}
        rows={pickRows}
      />
      <ModelSheet
        agent={sheet?.kind === 'model' ? sheet.agent : null}
        tierModels={sheet?.kind === 'model' ? (list.find((a) => a.agent === sheet.agent)?.tiers ?? {}) : {}}
        value={sheet?.kind === 'model' ? (prefs.models[sheet.agent] ?? '') : ''}
        onClose={() => setSheet({ kind: 'pick' })}
        onPick={(model) => {
          if (sheet?.kind !== 'model') return;
          const models = { ...prefs.models };
          if (model) models[sheet.agent] = model;
          else delete models[sheet.agent];
          change({ models });
          setSheet({ kind: 'pick' });
        }}
      />
      <TierSheet
        open={sheet?.kind === 'tier'}
        value={prefs.tier}
        models={tierModels}
        onClose={() => setSheet(null)}
        onPick={(tier) => {
          change({ tier });
          setSheet(null);
        }}
      />
      <AISheet
        open={sheet?.kind === 'ais' && !!sheetTurn}
        onClose={() => setSheet(null)}
        title={t('ask.aiCount', { n: resultViews.length })}
        subtitle={collecting !== null ? `${t('ask.collecting')} · ${t('ask.seconds', { s: collecting })}` : (sheetTurn && metaLine(sheetTurn)) || undefined}
        rows={resultRows}
      />
      <AnswerSheet
        p={answerView}
        judgedBy={answerView ? judgedBy(sheetSnap, answerView.agent) : []}
        onClose={() => setSheet(null)}
        onCopy={answerView ? () => copy(answerKey, answerView.part.answer ?? '') : undefined}
        onOnly={answerView ? onlyOf(answerView.agent, () => setSheet(null)) : undefined}
        onContinue={answerView ? carryOn(answerKey, answerView.part) : undefined}
        note={answerKey ? notes[answerKey] : undefined}
      />
      <PairSheet
        snap={sheetSnap}
        itemKey={sheet?.kind === 'pair' ? sheet.key : null}
        onClose={() => setSheet(null)}
        onShowAnswer={(agent) => sheetTurn && setSheet({ kind: 'answer', turn: sheetTurn.id, agent })}
      />
      <JudgesSheet turn={sheet?.kind === 'judges' ? sheetTurn : null} snap={sheetSnap} info={info} onClose={() => setSheet(null)} />
    </>
  );

  const header = <Header entrance="ask" right={turns.length ? <IconButton name="compose" label={t('ask.new')} onPress={fresh} /> : undefined} />;

  if (pcs.list === null) {
    return <View style={s.screen}>{header}</View>;
  }

  if (pcs.list.length === 0) {
    return (
      <View style={s.screen}>
        {header}
        <View style={s.noPC}>
          <Text style={s.title} accessibilityRole="header">
            {t('ask.noPC')}
          </Text>
          <Text style={s.body}>{t('ask.noPCHint')}</Text>
          <Button label={t('home.pair')} onPress={() => router.push('/pair')} />
        </View>
      </View>
    );
  }

  const reach =
    load.state === 'error' ? <ReachError cause={load.cause} detail={load.text} onRetry={() => pcRef.current && loadInfo(pcRef.current)} /> : null;

  if (!turns.length) {
    const recent = recentQuestions(3);
    return (
      <View style={s.screen}>
        {header}
        <ScrollView style={s.fill} contentContainerStyle={s.empty} keyboardShouldPersistTaps="handled">
          {reach}
          {load.state === 'loading' ? <Spinner size={24} /> : null}
          {info && !anyAsk ? <Text style={s.body}>{oldAgent(info) ? t('ask.oldAgent') : t('ask.noAI')}</Text> : null}
          {info && anyAsk ? (
            <>
              <View style={s.intro}>
                <Text style={s.display} accessibilityRole="header">
                  {t('ask.emptyTitle')}
                </Text>
                {/* Before the first question: where the answers go (R-34). */}
                <Text style={s.caption}>{judgeOn(info) && prefs.compare ? t('ask.introCompare') : t('ask.intro')}</Text>
                {canSynthesize(info) && prefs.synth ? <Text style={s.caption}>{t('ask.introSynth')}</Text> : null}
              </View>
              {recent.length ? (
                <View>
                  <Text style={s.section} accessibilityRole="header">
                    {t('ask.recent')}
                  </Text>
                  {recent.map((q) => (
                    <Pressable
                      key={q}
                      onPress={() => setText(q)}
                      accessibilityRole="button"
                      accessibilityLabel={q}
                      style={({ pressed }) => [s.recent, pressed && s.pressed]}
                    >
                      <Icon name="history" size={16} color={th.caption} />
                      <Text style={s.recentText} numberOfLines={1}>
                        {q}
                      </Text>
                    </Pressable>
                  ))}
                </View>
              ) : null}
            </>
          ) : null}
        </ScrollView>
        {info && anyAsk ? (
          <Composer
            value={text}
            onChangeText={setText}
            onSend={send}
            canSend={!!text.trim() && on.length > 0 && !busy}
            placeholder={t('ask.placeholder')}
            agents={on}
            pickLabel={on.length ? t('ask.pickAIs', { n: on.length, names: on.map(agentName).join(', ') }) : t('ask.pickAIsNone')}
            onPickAIs={() => setSheet({ kind: 'pick' })}
            tier={prefs.tier}
            onPickTier={() => setSheet({ kind: 'tier' })}
          />
        ) : null}
        {sheets}
      </View>
    );
  }

  const one = onlyTarget(to);
  const oneName = one ? agentName(one) : '';
  return (
    <View style={s.screen}>
      {header}
      <ScrollView
        ref={scroll}
        style={s.fill}
        contentContainerStyle={s.thread}
        keyboardShouldPersistTaps="handled"
        onContentSizeChange={() => {
          // To the new question once it is sent.
          if (!stick.current) return;
          stick.current = false;
          scroll.current?.scrollToEnd({ animated: true });
        }}
      >
        {reach}
        {turns.map((turn, i) => (
          <TurnView
            key={turn.id}
            turn={turn}
            info={info}
            now={now}
            only={i === turns.length - 1 ? one : null}
            notes={notes}
            ui={ui[turn.id] ?? noUI}
            act={actsFor(turn)}
          />
        ))}
      </ScrollView>
      <FollowBar
        value={text}
        onChangeText={setText}
        onSend={send}
        canSend={!!text.trim() && !busy && !!info && (one ? true : on.length > 0)}
        placeholder={one ? t('ask.followTo', { name: oneName }) : t('ask.followPlaceholder')}
        sendLabel={one ? t('ask.sendTo', { name: oneName }) : t('ask.sendIcon')}
        sendHint={busy ? t('ask.sendWait') : undefined}
        to={
          one
            ? { agent: one, label: t('ask.toOne', { name: oneName }), cancelLabel: t('ask.toOneCancel', { name: oneName }), onCancel: () => setTo('all') }
            : undefined
        }
      />
      {sheets}
    </View>
  );
}

// The AI picker's controls at the right of a row: the AI on or off and its
// model. The row is the switch for screen readers (UX-9), so the small
// switch is only for the eye and the finger; the › is its own button.
function PickControls({
  th,
  agent,
  name,
  on,
  disabled,
  onToggle,
  onModel,
}: {
  th: Theme;
  agent: string;
  name: string;
  on: boolean;
  disabled: boolean;
  onToggle: () => void;
  onModel: () => void;
}) {
  const s = styles(th);
  return (
    <View style={s.pickRight}>
      <Pressable
        onPress={onToggle}
        disabled={disabled}
        accessible={false}
        importantForAccessibility="no-hide-descendants"
        accessibilityElementsHidden
        testID={`switch-${agent}`}
        hitSlop={10}
        style={[s.track, { backgroundColor: on ? th.fill : th.stopped, justifyContent: on ? 'flex-end' : 'flex-start' }]}
      >
        <View style={[s.knob, { backgroundColor: on ? th.onFill : th.bg }]} />
      </Pressable>
      {disabled ? null : (
        <IconButton name="chevronRight" label={t('ask.modelTitle', { name })} hint={t('ask.pickModel')} onPress={onModel} size={18} color={th.caption} />
      )}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    fill: { flex: 1 },
    noPC: { flex: 1, justifyContent: 'center', gap: 12, paddingHorizontal: 24, paddingBottom: 56 },
    title: { fontSize: 18, fontWeight: '600', lineHeight: 24, letterSpacing: -0.18, color: th.text },
    body: { fontSize: 15, lineHeight: 22.5, color: th.textSub },
    empty: { flexGrow: 1, justifyContent: 'flex-end', gap: 28, paddingHorizontal: 16, paddingBottom: 20 },
    intro: { gap: 8, paddingHorizontal: 4 },
    display: { fontSize: 24, fontWeight: '600', lineHeight: 32, letterSpacing: -0.48, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    section: { marginBottom: 2, paddingHorizontal: 4, fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    recent: { minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 4, borderRadius: 12 },
    recentText: { flex: 1, minWidth: 0, fontSize: 15, lineHeight: 21, color: th.textSub },
    pressed: { backgroundColor: th.raised },
    thread: { gap: 28, paddingHorizontal: 20, paddingTop: 4, paddingBottom: 20 },
    pickRight: { flexDirection: 'row', alignItems: 'center', gap: 2, marginRight: -6 },
    track: { width: 40, height: 24, borderRadius: 12, padding: 3, flexDirection: 'row' },
    knob: { width: 18, height: 18, borderRadius: 9 },
  }),
);
