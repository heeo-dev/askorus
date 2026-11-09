// 한 번에 / All at once ('/', S5c): the app opens here. One question goes to
// the current PC's AIs at once (POST /v1/runs); while they answer, who is
// at it; once the answers are in, the moderator AI writes a combined answer
// from them (C6, #19), shown with the AIs' own answers as cards
// (F-Ask-Empty … F-Result-Light).
// Follow-ups go to the AIs picked in the follow-up bar (#22: by default
// the last question's AIs and tier; one AI is the old 'only this AI'), each
// carrying its own conversation on, or to the moderator alone (#25: its
// row on top of the picker, where the PC asks the moderator); Claude's
// answer can be carried on in the Claude app.
//
// thread.ts follows the runs and comparisons; compare.ts and present.ts
// say what each state shows; ui/TurnView draws a turn. This screen keeps
// the conversation, saved on the phone as it changes (milestone B): 'New
// question' and opening another one from the drawer archive it in
// ask/conversations.ts. A conversation stays with the PC it was asked on.
//
// #23 (A, the user's choice): a conversation left while it still runs is
// not stopped. It goes on in the background, kept in the history as it
// changes, and a question sent meanwhile waits (Turn.queued) until it is
// done. Only a removed conversation is stopped.
//
// #29: a follow-up carries the conversation record (record.ts), built when
// it goes; one sent while this conversation's combined answer is written
// or its debate runs waits for it and then goes with it (only when it
// carries a record). The first one that would carry a record shows, once,
// where it goes.

import * as Clipboard from 'expo-clipboard';
import * as Crypto from 'expo-crypto';
import { router, useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppState, Keyboard, Linking, Pressable, ScrollView, type ScrollViewInstance, StyleSheet, Text, View, type ViewInstance } from 'react-native';

import { agentName } from '@/api/agents';
import type { AgentClient } from '@/api/client';
import { type Info, type RunPart, type Tier, tiers } from '@/api/types';
import { canSynthesize, polling, writerFor } from '@/ask/compare';
import { conversations, recentQuestions, useConversations } from '@/ask/conversations';
import { features } from '@/ask/features';
import { modelLabel, pickOf, runsWith } from '@/ask/models';
import { aiStatus, askErrorText, metaLine, partViews, rowValue } from '@/ask/present';
import { type AskPrefs, defaultAskPrefs, loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { debating } from '@/ask/debate';
import { buildRecord, concluding, moderatorAnswers } from '@/ask/record';
import {
  askers,
  carry,
  type FollowDeps,
  followCompare,
  followDebate,
  followDefault,
  followRun,
  newTurn,
  running,
  stopTurns,
  type Turn,
} from '@/ask/thread';
import { connect } from '@/connections/open';
import { type Cause, cause } from '@/connections/reach';
import { type Connection, ConnectionStore } from '@/connections/store';
import { errorText, has, t } from '@/i18n';
import { AISheet, type AISheetRow } from '@/ui/AISheet';
import { AnswerSheet } from '@/ui/AnswerSheet';
import { Button } from '@/ui/Button';
import { Composer, FollowBar } from '@/ui/Composer';
import { Spinner } from '@/ui/controls';
import { DebateDock, DebateEnd } from '@/ui/Debate';
import { Header } from '@/ui/Header';
import { Icon, IconButton } from '@/ui/Icon';
import { ModelSheet } from '@/ui/ModelSheet';
import { RecordNotice, RecordSheet } from '@/ui/Record';
import { RefineSheet } from '@/ui/Refine';
import { ReachError } from '@/ui/ReachError';
import { usePCs } from '@/ui/shell/pcs';
import { themed, type Theme, useTheme } from '@/ui/theme';
import { TierSheet } from '@/ui/TierSheet';
import { type Note, type TurnActions, TurnView } from '@/ui/TurnView';

const store = new ConnectionStore();

type PC = { conn: Connection; client: AgentClient };
type Load = { state: 'loading' } | { state: 'ok'; info: Info } | { state: 'error'; cause: Cause; text: string };
type Head = { id: string; pcId: string; startedAt: number; title: string };
// The one sheet open over the screen.
type SheetState =
  | { kind: 'pick' }
  | { kind: 'model'; agent: string; back?: 'pick' | 'followPick' }
  | { kind: 'tier' }
  | { kind: 'followPick' }
  | { kind: 'followTier' }
  | { kind: 'ais'; turn: string }
  | { kind: 'answer'; turn: string; agent: string }
  | { kind: 'refine'; turn: string }
  | { kind: 'record'; turn: string }
  | null;

// An agent from before asks (P2-1) reports Claude alone and no tiers; a
// newer one always lists the AIs that only answer questions too.
function oldAgent(info: Info): boolean {
  return info.agents.every((a) => a.agent === 'claude' && !a.ask);
}

// A drag that ends with the debate's end mark at most this many points below
// the screen follows the debate again; when following, the mark sits this
// many points above the screen's bottom.
const followGap = 400;
const endRoom = 24;

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
  // The follow-up bar's AIs and tier (#22), picked for this conversation; null is its default.
  const [pick, setPick] = useState<{ agents: string[]; tier: Tier; moderator?: boolean } | null>(null);
  const [notes, setNotes] = useState<Record<string, Note>>({});
  const [sheet, setSheet] = useState<SheetState>(null);
  // The notice before the first follow-up that carries the record (#29).
  const [askRecord, setAskRecord] = useState(false);
  const follows = useRef(new Map<string, AbortController>());
  const pcRef = useRef<PC | null>(null);
  const turnsRef = useRef<Turn[]>([]);
  const head = useRef<Head | null>(null);
  const saving = useRef<Promise<unknown>>(Promise.resolve());
  const scroll = useRef<ScrollViewInstance>(null);
  const stick = useRef(false);
  turnsRef.current = turns;
  // #36: while a debate runs, the thread follows the debate's newest turn
  // (its end mark: on an earlier turn the debate is not the thread's end,
  // 11-09) unless the person drags away to read; it follows again when they
  // come back near it, when a debate starts and when its live card above the
  // follow-up bar (DebateDock) is pressed.
  const following = useRef(true);
  const debateEnd = useRef<ViewInstance | null>(null);
  const viewH = useRef(0);
  const offsetY = useRef(0);
  const markDebateEnd = useCallback((node: ViewInstance | null) => {
    debateEnd.current = node;
  }, []);
  // whereDebateEnds measures the end mark in the thread's content.
  const whereDebateEnds = useCallback((then: (y: number) => void) => {
    const end = debateEnd.current;
    const inner = scroll.current?.getInnerViewRef();
    if (!end || !inner) return false;
    end.measureLayout(inner, (_x, y) => then(y), () => undefined);
    return true;
  }, []);
  const toDebateEnd = useCallback(() => {
    const found = whereDebateEnds((y) => scroll.current?.scrollTo({ y: Math.max(0, y - viewH.current + endRoom), animated: true }));
    if (!found) scroll.current?.scrollToEnd({ animated: true });
  }, [whereDebateEnds]);
  const followAgainIfNear = () =>
    whereDebateEnds((y) => {
      if (y <= offsetY.current + viewH.current + followGap) following.current = true;
    });
  const liveDebate = turns.find(debating);
  const debateOn = !!liveDebate;
  useEffect(() => {
    if (!debateOn) return;
    following.current = true;
    toDebateEnd();
  }, [debateOn, toDebateEnd]);

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

  // Back on this screen: Settings may have changed the tier, the combined
  // answer or its writer, and the PCs may have changed.
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

  // Conversations left while they still ran (#23), by id: their follows
  // patch these copies, kept in the history at most about once a second.
  const bgs = useRef(new Map<string, { head: Head; turns: Turn[] }>());
  const [bgBusy, setBgBusy] = useState(false);
  const bgTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const keepBgs = useCallback(() => {
    if (bgTimer.current) return;
    bgTimer.current = setTimeout(() => {
      bgTimer.current = null;
      for (const b of bgs.current.values()) conversations.save({ ...b.head, turns: b.turns });
    }, 1000);
  }, []);

  // What following a run or a comparison needs; thread.ts does the following.
  const deps: FollowDeps = useMemo(
    () => ({
      follows: follows.current,
      update: (id, patch) => {
        for (const b of bgs.current.values()) {
          if (!b.turns.some((x) => x.id === id)) continue;
          b.turns = b.turns.map((x) => (x.id === id ? { ...x, ...patch(x) } : x));
          keepBgs();
          return;
        }
        setTurns((ts) => ts.map((x) => (x.id === id ? { ...x, ...patch(x) } : x)));
      },
      newId: () => Crypto.randomUUID(),
      errorText: askErrorText,
    }),
    [keepBgs],
  );

  // The follows going on, with their turns: a conversation with one still runs.
  const jobs = useRef(new Map<Promise<void>, string>());
  const track = useCallback((id: string, job: Promise<void>) => {
    jobs.current.set(job, id);
    job.finally(() => jobs.current.delete(job)).catch(() => undefined);
  }, []);
  const jobsOf = useCallback((ts: Turn[]) => [...jobs.current].filter(([, id]) => ts.some((x) => x.id === id)).map(([job]) => job), []);

  // What a question that waited needs when it goes: the PC's info and the prefs as they are then.
  const infoRef = useRef<Info | null>(null);
  const prefsRef = useRef<AskPrefs>(prefs);
  prefsRef.current = prefs;
  const started = useRef(new Set<string>());

  // A question goes now, as it was written (#23), timed from now, with the
  // conversation record as it is now (#29): one that waited (a debate it
  // waited for is in), or one that never went and is tried again (R-91,
  // 11-09: its seconds counted from when it was written, '390 s' for an
  // answer that took 27 s).
  const sendNow = useCallback(
    (client: AgentClient, x: Turn) => {
      const at = Date.now();
      const all = turnsRef.current;
      const i = all.findIndex((y) => y.id === x.id);
      const { req } = carry(x, i < 0 ? [] : all.slice(0, i), infoRef.current, prefsRef.current.carryRecord);
      deps.update(x.id, () => ({ queued: undefined, queuedFor: undefined, at, req }));
      track(x.id, followRun(client, { ...x, queued: undefined, queuedFor: undefined, at, req }, deps));
    },
    [deps, track],
  );
  const startQueued = useCallback(
    (client: AgentClient | undefined, x: Turn) => {
      if (started.current.has(x.id)) return;
      started.current.add(x.id);
      if (client) sendNow(client, x);
      else deps.update(x.id, () => ({ queued: undefined, queuedFor: undefined, error: t('ask.notSent') }));
    },
    [deps, sendNow],
  );

  // A conversation that ran in the background is done: it is kept as it
  // ended, and once none runs, the question that waited goes.
  const finishBg = useCallback(
    (id: string) => {
      const b = bgs.current.get(id);
      if (!b) return;
      bgs.current.delete(id);
      conversations.save({ ...b.head, turns: b.turns });
      if (bgs.current.size) return;
      if (bgTimer.current) clearTimeout(bgTimer.current);
      bgTimer.current = null;
      setBgBusy(false);
      // A combined answer or debate of this conversation still coming holds it back (#29).
      if (concluding(turnsRef.current)) return;
      for (const x of turnsRef.current) if (x.queued) startQueued(pcRef.current?.client, x);
    },
    [startQueued],
  );

  // A question that waited for this conversation's combined answer or
  // debate goes once it is in (#29), and nothing else runs in the background.
  useEffect(() => {
    const client = pcRef.current?.client;
    if (!client || bgs.current.size || concluding(turns)) return;
    for (const x of turns) if (x.queued) startQueued(client, x);
  }, [turns, startQueued]);

  // The current conversation is kept on the phone as it changes (milestone
  // B): at most about once a second, and at once when the app goes to the
  // background or another conversation takes its place.
  const pending = useRef<ReturnType<typeof setTimeout> | null>(null);
  const keepNow = useCallback(() => {
    if (pending.current) clearTimeout(pending.current);
    pending.current = null;
    const h = head.current;
    if (h && turnsRef.current.length) conversations.save({ ...h, turns: turnsRef.current });
  }, []);
  useEffect(() => {
    if (!head.current || !turns.length || pending.current) return;
    pending.current = setTimeout(keepNow, 1000);
  }, [turns, keepNow]);
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s !== 'active') keepNow();
    });
    return () => {
      sub.remove();
      keepNow();
    };
  }, [keepNow]);

  // 'New question': the conversation is kept in the history. One that still
  // runs goes on in the background (#23); a removed one (drop) is stopped
  // (the PC is asked to stop its comparisons still running) and not kept.
  // A question that waited and never went is kept as not sent.
  const fresh = useCallback((drop = false) => {
    const h = head.current;
    let current = turnsRef.current;
    if (current.some((x) => x.queued)) {
      current = current.map((x) => (x.queued ? { ...x, queued: undefined, error: t('ask.notSent') } : x));
      turnsRef.current = current;
    }
    const left = drop ? [] : jobsOf(current);
    if (h && left.length) {
      bgs.current.set(h.id, { head: h, turns: current });
      setBgBusy(true);
      Promise.allSettled(left).then(() => finishBg(h.id));
    } else {
      stopTurns(pcRef.current?.client, current, follows.current);
    }
    if (drop) {
      if (pending.current) clearTimeout(pending.current);
      pending.current = null;
    } else keepNow();
    head.current = null;
    conversations.setCurrent(null);
    turnsRef.current = [];
    setTurns([]);
    setNotes({});
    setSheet(null);
    setAskRecord(false);
    setPick(null);
  }, [keepNow, finishBg, jobsOf]);

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
  // the PC does it once), so nothing spins for ever. One still running in
  // the background (#23) comes back as it is, its follows going on.
  const reopen = useCallback(
    async (id: string) => {
      const back = bgs.current.get(id);
      if (back) {
        fresh();
        bgs.current.delete(id);
        setBgBusy(bgs.current.size > 0);
        head.current = back.head;
        conversations.setCurrent(back.head);
        turnsRef.current = back.turns;
        setTurns(back.turns);
        return;
      }
      const c = await conversations.load(id);
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
        // Kept while it waited (the app was closed): it goes once none runs in the background.
        if (turn.queued) {
          if (!bgs.current.size && !concluding(c.turns)) startQueued(p.client, turn);
          continue;
        }
        if (running(turn)) {
          track(turn.id, followRun(p.client, turn, deps, turn.run?.runId));
          continue;
        }
        const cmp = turn.compare;
        if (!cmp || turn.error || cmp.error) continue;
        if (cmp.key) track(turn.id, followCompare(p.client, turn, 'start', deps));
        else if (cmp.id && (!cmp.snap || cmp.snap.comparisonId !== cmp.id || polling(cmp.snap)))
          track(turn.id, followCompare(p.client, turn, 'poll', deps));
        // A debate that was on its way or still ran goes on being followed (P2-5).
        const dbt = turn.debate;
        if (!dbt || dbt.error) continue;
        // The combined answer it starts from is kept with the turn: no info is needed.
        if (dbt.key) track(turn.id, followDebate(p.client, turn, null, 'start', deps));
        else if (dbt.id && (!dbt.snap || dbt.snap.debateId !== dbt.id || dbt.snap.pollAfterMs))
          track(turn.id, followDebate(p.client, turn, null, 'poll', deps));
      }
    },
    [currentId, deps, fresh, pcs, startQueued, track],
  );

  useEffect(() => {
    if (!convs.openRequest) return;
    const id = conversations.takeOpenRequest();
    if (id) reopen(id);
  }, [convs.openRequest, reopen]);

  // The conversation on screen was removed (drawer or Settings): start afresh.
  useEffect(() => {
    if (!convs.dropped) return;
    const id = conversations.takeDropped();
    if (id && head.current?.id === id) fresh(true);
  }, [convs.dropped, fresh]);

  useEffect(() => {
    const current = follows.current;
    return () => current.forEach((c) => c.abort());
  }, []);

  const info = load.state === 'ok' && pc ? load.info : null;
  infoRef.current = info;
  const list = info ? askers(info) : [];
  const anyAsk = list.some((a) => a.canAsk);
  const on = list.filter((a) => a.canAsk && !prefs.off.includes(a.agent)).map((a) => a.agent);
  const busy = turns.some(running);
  const canOpen = info?.capabilities.includes('open_official') ?? false;
  const canAsk = (agent: string) => list.some((a) => a.agent === agent && a.canAsk);
  const now = Date.now();
  // Who the next follow-up goes to (#22): what the bar picked, else the last question's AIs and tier.
  const follow: { agents: string[]; tier: Tier; moderator?: boolean } = pick ?? followDefault(turns, on, canAsk, prefs.tier);
  // The moderator (#25): the moderator AI set, else the first AI that can answer, where the PC
  // asks the moderator and there are answers to talk about.
  const modAgent = info?.moderator ? (canAsk(prefs.synthAgent) ? prefs.synthAgent : (list.find((a) => a.canAsk)?.agent ?? null)) : null;
  const modOffered = !!modAgent && moderatorAnswers(turns).length > 0;
  const modAsk = !!follow.moderator && modOffered;
  // One AI alone (the old 'only this AI'), shown as its chip while others are on.
  const one = !modAsk && follow.agents.length === 1 && on.length > 1 ? follow.agents[0] : null;

  // record: the answer to the notice (#29), left out when none was shown.
  const sendWith = (record?: 'with' | 'without') => {
    const question = text.trim();
    const p = pcRef.current;
    if (!p || !info || !question || busy) return;
    // What this conversation concludes is still coming (its combined answer or debate): a
    // question that carries the record waits for it (#29); one sent without a record goes now.
    const coming = concluding(turns);
    const followUp = turns.length > 0;
    // A question to the moderator always reads the record and needs no notice (#25: its
    // company already gets every answer).
    const toModerator = followUp && modAsk && !!modAgent;
    const carries = toModerator || (prefs.carryRecord && !!info.context);
    if (!record && !toModerator && carries && !prefs.recordNoticed && (coming || buildRecord(turns, info))) {
      setAskRecord(true);
      return;
    }
    if (record) {
      setAskRecord(false);
      change({ recordNoticed: true });
    }
    // The first question goes to everyone turned on with the tier set; a follow-up as its bar says (#22).
    const modPick = toModerator && modAgent ? prefs.synthModels[modAgent] : undefined;
    let turn = newTurn({
      id: Crypto.randomUUID(),
      question,
      tier: followUp ? follow.tier : prefs.tier,
      to: followUp ? { agents: follow.agents } : 'all',
      on,
      turns,
      models: prefs.models,
      efforts: prefs.efforts,
      info,
      synth: { on: prefs.synth, agent: prefs.synthAgent, models: prefs.synthModels },
      refine: features.refine ? { on: prefs.refine, agent: prefs.synthAgent } : undefined,
      moderator:
        toModerator && modAgent
          ? { agent: modAgent, ...(modPick ? { model: modPick.model, ...(modPick.effort ? { effort: modPick.effort } : {}) } : {}), answers: moderatorAnswers(turns) }
          : undefined,
      at: Date.now(),
    });
    if (!turn.agents.length) return;
    if (record === 'without') turn.noRecord = true;
    // A conversation left running goes first: this question waits for it (#23).
    const waitBg = bgs.current.size > 0;
    const waitHere = carries && record !== 'without' && coming ? coming : null;
    const wait = waitBg || !!waitHere;
    if (wait) {
      turn.queued = true;
      if (!waitBg && waitHere) turn.queuedFor = waitHere;
    } else turn = carry(turn, turns, info, prefs.carryRecord);
    if (!head.current) {
      head.current = { id: Crypto.randomUUID(), pcId: p.conn.id, startedAt: Date.now(), title: question };
      conversations.setCurrent(head.current);
    }
    turnsRef.current = [...turnsRef.current, turn];
    setTurns((ts) => [...ts, turn]);
    setText('');
    setPick(null);
    Keyboard.dismiss();
    stick.current = true;
    if (!wait) track(turn.id, followRun(p.client, turn, deps));
  };
  const send = () => sendWith();

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

  const onlyOf = (agent: string, then?: () => void) => (canAsk(agent) ? () => (setPick({ agents: [agent], tier: follow.tier }), then?.()) : undefined);
  const carryOn = (key: string, p: RunPart) =>
    p.agent === 'claude' && p.session && canOpen ? () => continueInClaude(key, p.session ?? '') : undefined;

  const actsFor = (turn: Turn): TurnActions => ({
    compare: (how) => {
      const p = pcRef.current;
      if (!p) return;
      if (how === 'synth') track(turn.id, followCompare(p.client, turn, how, deps, { agent: writerFor(info, prefs.synthAgent) }));
      else track(turn.id, followCompare(p.client, turn, how, deps));
    },
    retryRun: () => {
      const p = pcRef.current;
      if (!p) return;
      // A run the PC has is picked up again; a question that never went goes now.
      if (turn.run?.runId) track(turn.id, followRun(p.client, turn, deps, turn.run.runId));
      else sendNow(p.client, turnsRef.current.find((y) => y.id === turn.id) ?? turn);
    },
    openAnswer: (agent) => setSheet({ kind: 'answer', turn: turn.id, agent }),
    openAIs: () => setSheet({ kind: 'ais', turn: turn.id }),
    openRefine: () => setSheet({ kind: 'refine', turn: turn.id }),
    openRecord: () => setSheet({ kind: 'record', turn: turn.id }),
    copy,
    only: (agent) => onlyOf(agent),
    carryOn,
    debate: {
      start: () => {
        const p = pcRef.current;
        if (p) track(turn.id, followDebate(p.client, turn, info, 'start', deps));
      },
      stop: () => {
        const p = pcRef.current;
        if (p) followDebate(p.client, turn, info, 'stop', deps);
      },
      poll: () => {
        const p = pcRef.current;
        if (p) track(turn.id, followDebate(p.client, turn, info, 'poll', deps));
      },
      copy,
      openAIs: () => setSheet({ kind: 'ais', turn: turn.id }),
      noticed: prefs.debateNoticed,
      notice: () => change({ debateNoticed: true }),
    },
  });

  // The last AI that is on stays on (UX-10): a question always has an AI to go to.
  const toggle = (agent: string) => {
    if (on.length === 1 && on[0] === agent) return;
    change({ off: prefs.off.includes(agent) ? prefs.off.filter((x) => x !== agent) : [...prefs.off, agent] });
  };
  // The same in the follow-up bar's picker (#22), for this conversation only, in the PC's order.
  const toggleFollow = (agent: string) => {
    // From the moderator back to the AIs: this one.
    if (modAsk) return setPick({ agents: [agent], tier: follow.tier });
    const has = follow.agents.includes(agent);
    if (has && follow.agents.length === 1) return;
    const next = has ? follow.agents.filter((x) => x !== agent) : list.map((a) => a.agent).filter((a) => a === agent || follow.agents.includes(a));
    setPick({ agents: next, tier: follow.tier });
  };

  // ---- The sheets ----

  const sheetTurn = sheet && 'turn' in sheet ? (turns.find((x) => x.id === sheet.turn) ?? null) : null;
  // What an AI answers with now: its own pick, else the tier's model and effort.
  const asker = (agent: string) => list.find((a) => a.agent === agent);
  const modelOf = (agent: string, tier: Tier = prefs.tier) => {
    const m = runsWith(pickOf(prefs, agent), asker(agent)?.tiers ?? {}, tier);
    return m ? modelLabel(m) : '';
  };
  // Per tier, the models and efforts of the AIs that are on (or picked for the follow-up) and follow the tier (#16).
  const tierModelsOf = (agents: string[]) => Object.fromEntries(
    tiers.map((x) => {
      const line = agents
        .filter((a) => !prefs.models[a])
        .map((a) => {
          const m = asker(a)?.tiers[x];
          return m?.model ? `${agentName(a)} ${modelLabel(m)}` : '';
        })
        .filter(Boolean)
        .join(', ');
      return [x, line];
    }),
  ) as Record<Tier, string>;
  const tierModels = tierModelsOf(on);

  const pickRows: AISheetRow[] = list.map((a) => {
    const name = agentName(a.agent);
    return {
      agent: a.agent,
      name,
      model: a.canAsk ? modelOf(a.agent) || undefined : t('ask.error.agent_cannot_ask'),
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

  // The follow-up bar's picker (#22): the same rows, switching the AIs of the next follow-up,
  // under the moderator's row (#25: it goes alone, never with the AIs).
  const modRow: AISheetRow[] =
    modOffered && modAgent
      ? [
          {
            agent: modAgent,
            name: t('ask.moderatorRow'),
            model: t('ask.moderatorRowHint', { name: agentName(modAgent) }),
            onPress: () => setPick({ agents: follow.agents, tier: follow.tier, moderator: !modAsk }),
            checked: modAsk,
            // A switch like the AIs' rows, without a model ›: its model is Settings' moderator model.
            right: (
              <PickControls
                th={th}
                agent="moderator"
                name={t('ask.moderatorRow')}
                on={modAsk}
                disabled={false}
                onToggle={() => setPick({ agents: follow.agents, tier: follow.tier, moderator: !modAsk })}
              />
            ),
            accessibilityValue: { text: '' },
          },
        ]
      : [];
  const followRows: AISheetRow[] = modRow.concat(list.map((a) => {
    const name = agentName(a.agent);
    const picked = !modAsk && follow.agents.includes(a.agent);
    return {
      agent: a.agent,
      name,
      model: a.canAsk ? modelOf(a.agent, follow.tier) || undefined : t('ask.error.agent_cannot_ask'),
      onPress: a.canAsk ? () => toggleFollow(a.agent) : undefined,
      onLongPress: a.canAsk ? () => setSheet({ kind: 'model', agent: a.agent, back: 'followPick' }) : undefined,
      disabled: !a.canAsk,
      checked: picked,
      right: (
        <PickControls
          th={th}
          agent={a.agent}
          name={name}
          on={picked}
          disabled={!a.canAsk}
          onToggle={() => toggleFollow(a.agent)}
          onModel={() => setSheet({ kind: 'model', agent: a.agent, back: 'followPick' })}
        />
      ),
      accessibilityValue: { text: '' },
    };
  }));

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
        tierModels={sheet?.kind === 'model' ? (asker(sheet.agent)?.tiers ?? {}) : {}}
        efforts={sheet?.kind === 'model' ? (asker(sheet.agent)?.efforts ?? {}) : {}}
        tier={prefs.tier}
        value={sheet?.kind === 'model' ? pickOf(prefs, sheet.agent) : null}
        onClose={() => setSheet({ kind: sheet?.kind === 'model' && sheet.back ? sheet.back : 'pick' })}
        onPick={(pick, close) => {
          if (sheet?.kind !== 'model') return;
          const models = { ...prefs.models };
          const efforts = { ...prefs.efforts };
          delete models[sheet.agent];
          delete efforts[sheet.agent];
          if (pick) {
            models[sheet.agent] = pick.model;
            if (pick.effort) efforts[sheet.agent] = pick.effort;
          }
          change({ models, efforts });
          if (close) setSheet({ kind: sheet.back ?? 'pick' });
        }}
      />
      <AISheet
        open={sheet?.kind === 'followPick'}
        onClose={() => setSheet(null)}
        title={t('ask.followPickTitle')}
        subtitle={pc ? t('ask.onPC', { name: pc.conn.name }) : undefined}
        rows={followRows}
      />
      <TierSheet
        open={sheet?.kind === 'followTier'}
        value={follow.tier}
        models={tierModelsOf(follow.agents)}
        onClose={() => setSheet(null)}
        onPick={(tier) => {
          setPick({ agents: follow.agents, tier });
          setSheet(null);
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
        onClose={() => setSheet(null)}
        onCopy={answerView ? () => copy(answerKey, answerView.part.answer ?? '') : undefined}
        onOnly={answerView ? onlyOf(answerView.agent, () => setSheet(null)) : undefined}
        onContinue={answerView ? carryOn(answerKey, answerView.part) : undefined}
        note={answerKey ? notes[answerKey] : undefined}
      />
      <RefineSheet refine={sheet?.kind === 'refine' ? (sheetTurn?.run?.refine ?? null) : null} onClose={() => setSheet(null)} />
      <RecordSheet context={sheet?.kind === 'record' ? (sheetTurn?.req.context ?? null) : null} onClose={() => setSheet(null)} />
    </>
  );

  const header = <Header entrance="ask" right={turns.length ? <IconButton name="compose" label={t('ask.new')} onPress={() => fresh()} /> : undefined} />;

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
                <Text style={s.caption}>{t('ask.intro')}</Text>
                {canSynthesize(info) && prefs.synth ? <Text style={s.caption}>{t('ask.introModerator')}</Text> : null}
                {bgBusy ? <Text style={s.caption}>{t('ask.goingOn')}</Text> : null}
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

  const oneName = one ? agentName(one) : '';
  const followNames = follow.agents.map(agentName).join(', ');
  return (
    <View style={s.screen}>
      {header}
      <DebateEnd.Provider value={markDebateEnd}>
        <ScrollView
          ref={scroll}
          style={s.fill}
          contentContainerStyle={s.thread}
          keyboardShouldPersistTaps="handled"
          scrollEventThrottle={100}
          onLayout={(e) => {
            viewH.current = e.nativeEvent.layout.height;
          }}
          onScroll={(e) => {
            offsetY.current = e.nativeEvent.contentOffset.y;
            viewH.current = e.nativeEvent.layoutMeasurement.height;
          }}
          onScrollBeginDrag={() => {
            following.current = false;
          }}
          onScrollEndDrag={followAgainIfNear}
          onMomentumScrollEnd={followAgainIfNear}
          onContentSizeChange={() => {
            // To the new question once it is sent.
            if (stick.current) {
              stick.current = false;
              scroll.current?.scrollToEnd({ animated: true });
              return;
            }
            // A new turn or outcome in a running debate.
            if (debateOn && following.current) toDebateEnd();
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
              act={actsFor(turn)}
            />
          ))}
        </ScrollView>
      </DebateEnd.Provider>
      {liveDebate ? (
        <DebateDock
          turn={liveDebate}
          onPress={() => {
            following.current = true;
            toDebateEnd();
          }}
        />
      ) : null}
      {askRecord ? <RecordNotice onGo={() => sendWith('with')} onWithout={() => sendWith('without')} /> : null}
      <FollowBar
        value={text}
        onChangeText={setText}
        onSend={send}
        canSend={!!text.trim() && !busy && !!info && follow.agents.length > 0}
        placeholder={modAsk ? t('ask.followModerator') : one ? t('ask.followTo', { name: oneName }) : t('ask.followPlaceholder')}
        sendLabel={modAsk ? t('ask.sendModerator') : one ? t('ask.sendTo', { name: oneName }) : t('ask.sendIcon')}
        sendHint={busy ? t('ask.sendWait') : undefined}
        to={
          modAsk && modAgent
            ? {
                agent: modAgent,
                label: t('ask.toModerator'),
                cancelLabel: t('ask.toModeratorCancel'),
                onCancel: () => setPick({ agents: follow.agents, tier: follow.tier }),
              }
            : one
            ? {
                agent: one,
                label: t('ask.toOne', { name: oneName }),
                cancelLabel: t('ask.toOneCancel', { name: oneName }),
                onCancel: () => setPick({ agents: on, tier: follow.tier }),
              }
            : undefined
        }
        agents={follow.agents}
        pickLabel={modAsk ? t('ask.toModerator') : t('ask.followPick', { n: follow.agents.length, names: followNames })}
        onPickAIs={() => setSheet({ kind: 'followPick' })}
        tier={follow.tier}
        onPickTier={() => setSheet({ kind: 'followTier' })}
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
  /** The model sheet; left out (the moderator's row, #25) the row has no ›. */
  onModel?: () => void;
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
      {disabled ? null : onModel ? (
        <IconButton name="chevronRight" label={t('ask.modelTitle', { name })} hint={t('ask.pickModel')} onPress={onModel} size={18} color={th.caption} />
      ) : (
        // No model › (the moderator's row): the space it takes, so the switches line up.
        <View style={s.noModel} />
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
    noModel: { width: 44, height: 44 },
    track: { width: 40, height: 24, borderRadius: 12, padding: 3, flexDirection: 'row' },
    knob: { width: 18, height: 18, borderRadius: 9 },
  }),
);
