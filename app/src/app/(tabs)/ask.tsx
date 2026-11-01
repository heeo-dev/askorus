// 한 번에 묻기 / Ask all at once (Phase 2, P2-2): one question to the PC's
// AIs at once (POST /v1/runs), each answer as it comes, follow-ups to
// everyone or one AI on its own session, and Claude's answer carried on in
// the Claude app. The conversation is kept in memory; the history is P2-6.
//
// P2-3: a turn to every AI turned on, on a PC whose AIs judge, gets tabs:
// 정리 (Summary) first, where the AIs' judgments of each other's answers
// show as cards, then one tab per AI with its answer card. thread.ts follows
// the run and the comparison; this screen wires them and draws.

import * as Clipboard from 'expo-clipboard';
import * as Crypto from 'expo-crypto';
import { router, useFocusEffect } from 'expo-router';
import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Keyboard, Linking, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { agentName } from '@/api/agents';
import { type AgentClient, AgentError } from '@/api/client';
import { type Info, type RunPart, tiers } from '@/api/types';
import { answered, judgedBy, type TabView, tabs } from '@/ask/compare';
import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import {
  type Asker,
  askers,
  type FollowDeps,
  followCompare,
  followRun,
  newTurn,
  parts,
  running,
  stopAll,
  type Target,
  type Turn,
} from '@/ask/thread';
import { connect } from '@/connections/open';
import { type Cause, cause } from '@/connections/reach';
import { type Connection, ConnectionStore } from '@/connections/store';
import { errorText, has, t } from '@/i18n';
import { AnswerCard, partError } from '@/ui/AnswerCard';
import { Button } from '@/ui/Button';
import { Choices } from '@/ui/Choices';
import { CompareSection } from '@/ui/CompareSection';
import { FormScroll } from '@/ui/FormScroll';
import { JudgesSheet } from '@/ui/JudgesSheet';
import { ModelSheet } from '@/ui/ModelSheet';
import { PairSheet } from '@/ui/PairSheet';
import { ReachError } from '@/ui/ReachError';
import { type Theme, useTheme } from '@/ui/theme';
import { TurnTabs } from '@/ui/TurnTabs';

const store = new ConnectionStore();

type PC = { conn: Connection; client: AgentClient };

// An agent from before asks (P2-1) reports Claude alone and no tiers; a
// newer one always lists the AIs that only answer questions too.
function oldAgent(info: Info): boolean {
  return info.agents.every((a) => a.agent === 'claude' && !a.ask);
}

// The PC's error codes in ask words where an ask needs other words (for
// example the ask folder that needs trusting once).
function askErrorText(e: unknown): string {
  return e instanceof AgentError ? partError({ code: e.code, message: e.message }) : errorText(e);
}
type Load = { state: 'loading' } | { state: 'ok'; info: Info } | { state: 'error'; cause: Cause; text: string };
type Note = { text: string; error?: boolean };
// The sheet open over the turns: one Summary item's, or who judged.
type Sheet = { turn: string; kind: 'pair'; key: string } | { turn: string; kind: 'judges' } | null;

const judgeOn = (info: Info) => info.agents.some((a) => a.available && a.capabilities.includes('judge'));

export default function Ask() {
  const th = useTheme();
  const s = styles(th);
  const [conns, setConns] = useState<Connection[] | null>(null);
  const [prefs, setPrefs] = useState<AskPrefs>(defaultAskPrefs);
  const [pc, setPC] = useState<PC | null>(null);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [turns, setTurns] = useState<Turn[]>([]);
  const [text, setText] = useState('');
  const [to, setTo] = useState<Target>('all');
  const [picking, setPicking] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, Note>>({});
  const [tabOf, setTabOf] = useState<Record<string, string>>({}); // turn id -> 'summary' or an AI
  const [sheet, setSheet] = useState<Sheet>(null);
  const follows = useRef(new Map<string, AbortController>());

  // Back on the tab: the PCs or the default tier may have changed.
  useFocusEffect(
    useCallback(() => {
      store.list().then(setConns);
      loadAskPrefs().then(setPrefs);
    }, []),
  );

  const pcId = conns?.find((c) => c.id === prefs.pc)?.id ?? conns?.[0]?.id ?? '';

  const loadInfo = useCallback(async (p: PC) => {
    setLoad({ state: 'loading' });
    try {
      setLoad({ state: 'ok', info: await p.client.info() });
    } catch (e) {
      setLoad({ state: 'error', cause: cause(e), text: errorText(e) });
    }
  }, []);

  useEffect(() => {
    if (!pcId) {
      setPC(null);
      return;
    }
    if (pc?.conn.id === pcId) return;
    connect(store, pcId).then((p) => {
      setPC(p);
      if (p) loadInfo(p);
    });
  }, [pcId, pc?.conn.id, loadInfo]);

  useEffect(() => {
    const current = follows.current;
    return () => current.forEach((c) => c.abort());
  }, []);

  const change = (next: AskPrefs) => {
    setPrefs(next);
    saveAskPrefs(next);
  };

  // What following a run or a comparison needs; thread.ts does the following.
  const deps: FollowDeps = {
    follows: follows.current,
    update: (id, patch) => setTurns((ts) => ts.map((x) => (x.id === id ? { ...x, ...patch(x) } : x))),
    newId: () => Crypto.randomUUID(),
    errorText: askErrorText,
  };

  if (conns === null) return <View style={s.screen} />;

  if (conns.length === 0) {
    return (
      <View style={[s.screen, s.pad]}>
        <Text style={s.title}>{t('ask.noPC')}</Text>
        <Text style={s.muted}>{t('ask.noPCHint')}</Text>
        <Button label={t('home.pair')} onPress={() => router.push('/pair')} />
      </View>
    );
  }

  const info = load.state === 'ok' ? load.info : null;
  const list: Asker[] = info ? askers(info) : [];
  const anyAsk = list.some((a) => a.canAsk);
  const on = list.filter((a) => a.canAsk && !prefs.off.includes(a.agent)).map((a) => a.agent);
  const busy = turns.some(running);
  const tierModels = (agent: string) => list.find((a) => a.agent === agent)?.tiers ?? {};
  const canOpen = info?.capabilities.includes('open_official') ?? false;

  const send = () => {
    const question = text.trim();
    if (!pc || !info || !question || busy) return;
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
      at: Date.now(),
    });
    if (!turn.agents.length) return;
    setTurns((ts) => [...ts, turn]);
    setText('');
    setTo('all');
    Keyboard.dismiss();
    followRun(pc.client, turn, deps);
  };

  // A new question stops following everything and asks the PC to stop the
  // comparisons still running.
  const fresh = () => {
    stopAll(pc?.client, turns, follows.current);
    setTurns([]);
    setNotes({});
    setTabOf({});
    setSheet(null);
    setTo('all');
  };

  const note = (key: string, n: Note | null) =>
    setNotes((m) => {
      const next = { ...m };
      if (n) next[key] = n;
      else delete next[key];
      return next;
    });

  // Carries Claude's ask session on in the Claude app, with the same rules
  // as opening a session from My PCs.
  const continueInClaude = async (key: string, session: string) => {
    if (!pc) return;
    note(key, { text: t('ask.opening') });
    try {
      const r = await pc.client.openSession(session);
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

  const card = (turn: Turn, p: RunPart) => {
    const key = `${turn.id}/${p.agent}`;
    return (
      <AnswerCard
        key={key}
        part={p}
        note={notes[key]}
        onCopy={p.answer ? () => copy(key, p.answer ?? '') : undefined}
        onOnly={list.some((a) => a.agent === p.agent && a.canAsk) ? () => setTo({ agent: p.agent }) : undefined}
        onContinue={p.agent === 'claude' && p.session && canOpen ? () => continueInClaude(key, p.session ?? '') : undefined}
      />
    );
  };

  // A compared turn: how many answered, the tabs, and the picked tab.
  const compared = (turn: Turn, tv: TabView[]) => {
    const picked = tabOf[turn.id] ?? 'summary';
    const tab = tv.some((x) => x.key === picked) ? picked : 'summary';
    const c = turn.compare;
    const snap = c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
    const part = tab === 'summary' ? undefined : parts(turn).find((p) => p.agent === tab);
    const by = part ? judgedBy(snap, part.agent) : [];
    const n = answered(turn);
    return (
      <>
        {/* Before the first answer the Summary tab says who is answering instead. */}
        {n.n ? <Text style={s.muted}>{t('compare.answered', n)}</Text> : null}
        <TurnTabs tabs={tv} value={tab} onChange={(k) => setTabOf((m) => ({ ...m, [turn.id]: k }))} />
        {part ? (
          <Fragment key={part.agent}>
            <Text style={s.muted}>{by.length ? t('compare.aiLine', { names: by.join(', ') }) : t('compare.via.pc')}</Text>
            {card(turn, part)}
          </Fragment>
        ) : (
          <CompareSection
            key={turn.id}
            turn={turn}
            info={info}
            onAction={(how) => pc && followCompare(pc.client, turn, how, deps)}
            onPair={(key) => setSheet({ turn: turn.id, kind: 'pair', key })}
            onJudges={() => setSheet({ turn: turn.id, kind: 'judges' })}
          />
        )}
      </>
    );
  };

  const sheetSnap = sheet ? turns.find((x) => x.id === sheet.turn)?.compare?.snap : undefined;

  const toggle = (agent: string) => {
    const off = prefs.off.includes(agent) ? prefs.off.filter((x) => x !== agent) : [...prefs.off, agent];
    change({ ...prefs, off });
  };

  return (
    <View style={s.screen}>
      <FormScroll contentContainerStyle={s.body}>
        {conns.length > 1 ? (
          <View style={s.block}>
            <Text style={s.label}>{t('ask.pc')}</Text>
            <Choices
              options={conns.map((c) => c.id)}
              value={pcId}
              onChange={(id) => change({ ...prefs, pc: id })}
              labelOf={(id) => conns.find((c) => c.id === id)?.name ?? id}
            />
          </View>
        ) : null}

        {load.state === 'error' ? (
          <ReachError cause={load.cause} detail={load.text} onRetry={() => pc && loadInfo(pc)} />
        ) : null}
        {load.state === 'loading' ? <ActivityIndicator color={th.accent} /> : null}
        {info && !anyAsk ? <Text style={s.muted}>{oldAgent(info) ? t('ask.oldAgent') : t('ask.noAI')}</Text> : null}

        {turns.length === 0 && info && anyAsk ? (
          // With auto compare on a PC whose AIs judge, the intro also says where the answers go.
          <Text style={s.muted}>{judgeOn(info) && prefs.compare ? t('ask.introCompare') : t('ask.intro')}</Text>
        ) : null}

        {turns.map((turn) => {
          const tv = tabs(turn);
          return (
            <View key={turn.id} style={s.turn}>
              <Text style={s.question} selectable>
                {turn.question}
              </Text>
              {tv ? compared(turn, tv) : parts(turn).map((p) => card(turn, p))}
              {turn.error ? (
                <View style={s.lost}>
                  <Text style={s.error}>{turn.error}</Text>
                  <Button
                    secondary
                    label={t('ask.retry')}
                    onPress={() => pc && followRun(pc.client, turn, deps, turn.run?.runId)}
                  />
                </View>
              ) : null}
            </View>
          );
        })}

        {info && anyAsk ? (
          <View style={s.composer}>
            <View style={s.chips} accessibilityHint={t('ask.longPress')}>
              {list.map((a) => {
                const isOn = on.includes(a.agent);
                const model = prefs.models[a.agent];
                return (
                  <Pressable
                    key={a.agent}
                    onPress={() => a.canAsk && toggle(a.agent)}
                    onLongPress={() => a.canAsk && setPicking(a.agent)}
                    disabled={!a.canAsk}
                    style={[s.chip, isOn && s.chipOn, !a.canAsk && s.chipOff]}
                    accessibilityRole="switch"
                    accessibilityState={{ checked: isOn, disabled: !a.canAsk }}
                    accessibilityLabel={agentName(a.agent)}
                  >
                    <Text style={[s.chipText, isOn && s.chipTextOn]}>
                      {agentName(a.agent)}
                      {model ? ` · ${model}` : ''}
                    </Text>
                  </Pressable>
                );
              })}
            </View>
            {/* Above the box, so the box is followed right by its button and
                both stay above the keyboard (the S10 hid the button). */}
            <Choices
              options={tiers}
              value={prefs.tier}
              onChange={(tier) => change({ ...prefs, tier })}
              labelOf={(x) => t(`ask.tier.${x}`)}
            />
            {to !== 'all' ? (
              <Pressable onPress={() => setTo('all')} accessibilityRole="button" style={s.toTag}>
                <Text style={s.toText}>{t('ask.toOne', { name: agentName(to.agent) })} ✕</Text>
              </Pressable>
            ) : null}
            <TextInput
              style={s.input}
              value={text}
              onChangeText={setText}
              placeholder={turns.length ? t('ask.followPlaceholder') : t('ask.placeholder')}
              placeholderTextColor={th.muted}
              multiline
              accessibilityLabel={t('ask.placeholder')}
            />
            <Button
              label={turns.length ? t('ask.sendFollow') : t('ask.send')}
              onPress={send}
              disabled={!text.trim() || busy || (to === 'all' ? on.length === 0 : false)}
            />
            {turns.length ? <Button secondary label={t('ask.new')} onPress={fresh} /> : null}
          </View>
        ) : null}
      </FormScroll>

      <ModelSheet
        agent={picking}
        tierModels={picking ? tierModels(picking) : {}}
        value={picking ? (prefs.models[picking] ?? '') : ''}
        onClose={() => setPicking(null)}
        onPick={(model) => {
          if (!picking) return;
          const models = { ...prefs.models };
          if (model) models[picking] = model;
          else delete models[picking];
          change({ ...prefs, models });
          setPicking(null);
        }}
      />
      <PairSheet
        snap={sheetSnap}
        itemKey={sheet?.kind === 'pair' ? sheet.key : null}
        onClose={() => setSheet(null)}
        onShowAnswer={(agent) => {
          if (sheet) setTabOf((m) => ({ ...m, [sheet.turn]: agent }));
          setSheet(null);
        }}
      />
      <JudgesSheet snap={sheetSnap} open={sheet?.kind === 'judges'} onClose={() => setSheet(null)} />
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    pad: { padding: 24, gap: 12, justifyContent: 'center' },
    body: { padding: 16, gap: 14 },
    block: { gap: 8 },
    label: { color: th.muted, fontSize: 13 },
    title: { color: th.text, fontSize: 17, fontWeight: '600' },
    muted: { color: th.muted, fontSize: 14, lineHeight: 20 },
    error: { color: th.danger, fontSize: 14, lineHeight: 20 },
    turn: { gap: 10 },
    question: {
      alignSelf: 'flex-end',
      maxWidth: '90%',
      backgroundColor: th.accent,
      color: th.accentText,
      fontSize: 15,
      lineHeight: 21,
      borderRadius: 14,
      paddingHorizontal: 14,
      paddingVertical: 10,
      overflow: 'hidden',
    },
    lost: { gap: 8 },
    composer: { gap: 10, paddingTop: 4 },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    chip: {
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 999,
      paddingHorizontal: 14,
      paddingVertical: 7,
      backgroundColor: th.card,
    },
    chipOn: { backgroundColor: th.accent, borderColor: th.accent },
    chipOff: { opacity: 0.4 },
    chipText: { color: th.text, fontSize: 14 },
    chipTextOn: { color: th.accentText, fontWeight: '600' },
    toTag: { alignSelf: 'flex-start', paddingVertical: 2 },
    toText: { color: th.accent, fontSize: 14, fontWeight: '600' },
    input: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 10,
      color: th.text,
      fontSize: 15,
      paddingHorizontal: 12,
      paddingVertical: 10,
      minHeight: 48,
      maxHeight: 160,
      textAlignVertical: 'top',
    },
  });
