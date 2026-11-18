// S5c: what the Ask screen shows of a turn (present.ts): the AIs' names,
// models, times and previews, and the combined answer's title and step
// (since C6 the moderator's alone: no judging, so no judging step).

import { agentName } from '@/api/agents';
import type { Comparison, Info, RunPart, Synthesis } from '@/api/types';
import { setLang } from '@/i18n';

import {
  aiStatus,
  answeredAgents,
  blockTitle,
  cardPreview,
  cutText,
  displayModel,
  metaLine,
  partError,
  partViews,
  plainPreview,
  rowValue,
  steps,
  synthExpected,
  turnTimes,
  whenAgain,
} from './present';
import { synthWriters } from './prefs';
import type { Turn, TurnCompare } from './thread';

afterEach(() => setLang('en'));

// ---- Fixtures ----

const texts = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};

// A comparison as the agent makes it since C6: done once the answers are
// in (running before), with the moderator's synthesis.
function comparison(o: { running?: boolean; synthesis?: Synthesis } = {}): Comparison {
  const writing = o.synthesis?.status === 'waiting' || o.synthesis?.status === 'running';
  const s: Comparison = { comparisonId: 'cmp-1', status: o.running ? 'running' : 'done', source: 'run', engine: 'pc' };
  if (o.running || writing) s.pollAfterMs = 1000;
  if (o.synthesis) s.synthesis = o.synthesis;
  return s;
}

const part = (agent: string, over: Partial<RunPart> = {}): RunPart => ({
  agent,
  status: 'done',
  model: `${agent}-std`,
  answer: texts[agent as keyof typeof texts],
  ms: 2000,
  ...over,
});

function turn(o: { parts?: RunPart[]; compare?: Partial<TurnCompare> | null; snap?: Comparison; error?: string; running?: boolean } = {}): Turn {
  const agents = ['claude', 'codex', 'opencode'];
  const t: Turn = {
    id: 't1',
    question: 'Tell me about Seoul',
    req: { mode: 'ask', prompt: 'Tell me about Seoul', tier: 'standard' },
    agents,
    at: 1_000_000,
    run: {
      runId: 'run-1',
      mode: 'ask',
      status: o.running ? 'running' : 'done',
      startedAt: 'x',
      parts: o.parts ?? agents.map((a) => part(a)),
    },
  };
  if (o.error) t.error = o.error;
  if (o.compare !== null) {
    t.compare = { start: 'auto', judges: agents, earlier: [], synth: { agent: 'claude' }, id: 'cmp-1', ...o.compare };
    if (o.snap) t.compare.snap = o.snap;
  }
  return t;
}

const info = (): Info => ({
  name: 'askorus-agent',
  version: '0.1.0',
  apiVersion: 1,
  hostname: 'PC',
  os: 'windows',
  agents: ['claude', 'codex', 'opencode'].map((agent) => ({ agent, available: true, capabilities: ['ask', 'synthesize'] })),
  capabilities: ['ask', 'synthesize'],
  reserved: [],
});

const waiting: Synthesis = { status: 'waiting', agent: 'claude' };
const writing: Synthesis = { status: 'running', agent: 'claude', model: 'claude-sonnet-5' };
const written: Synthesis = { status: 'done', agent: 'claude', model: 'claude-sonnet-5', ms: 15200, text: 'Fake combined answer.' };
const step = { key: 'synth', label: 'Writing the combined answer…' };

// ---- Models, times, previews ----

test('a model shows without its variant and provider (R-39)', () => {
  expect(displayModel('opencode-go/deepseek-v4.1-flash#high')).toBe('deepseek-v4.1-flash');
  expect(displayModel('deepseek-v4.1-flash#max')).toBe('deepseek-v4.1-flash');
  expect(displayModel('anthropic/claude-haiku-5-5')).toBe('claude-haiku-5-5');
  expect(displayModel('a/b/c#x#y')).toBe('c');
  expect(displayModel('gpt-6-luna')).toBe('gpt-6-luna');
  expect(displayModel(' sonnet ')).toBe('sonnet');
  expect(displayModel('#high')).toBe('#high'); // nothing left: as given
  expect(displayModel('')).toBe('');
  expect(displayModel(undefined)).toBe('');
  // Every place a turn shows a model uses it.
  const v = partViews(turn({ parts: [part('opencode', { model: 'opencode-go/deepseek-v4.1-flash#high' })] }), 0);
  expect(v.find((p) => p.agent === 'opencode')?.model).toBe('deepseek-v4.1-flash');
});

test('an answer preview is its plain text, cut to two lines worth', () => {
  expect(plainPreview('# Seoul\n\n**Seoul** is the *capital*.\n\n- About 9.4 million\n- [Wiki](https://x.y)')).toBe(
    'Seoul Seoul is the capital. About 9.4 million Wiki',
  );
  expect(plainPreview('> quoted `code $HOME/x:$PATH` here')).toBe('quoted code $HOME/x:$PATH here');
  expect(plainPreview('Area is $\\pi r^2$.')).toBe('Area is πr².');
  expect(plainPreview('| a | b |\n|---|---|\n| 1 | 2 |')).toBe('a b 1 2');
  expect(plainPreview('```js\nlet x = 1;\n```')).toBe('let x = 1;');
  expect(plainPreview('snake_case and 2*3*4 stay; ~~gone~~ marks &amp; <b>tags</b>')).toBe('snake_case and 2*3*4 stay; gone marks & tags');
  const long = plainPreview('가'.repeat(300));
  expect(long).toHaveLength(160);
  expect(long.endsWith('…')).toBe(true);
  expect(cutText('ab😀', 3)).toBe('ab…'); // never half an emoji
  expect(cutText('short', 160)).toBe('short');
});

test('the AIs of a turn: names, models, seconds and a value that is always an object (R-40)', () => {
  const t = turn({
    running: true,
    parts: [part('claude', { ms: 9400 }), part('codex', { status: 'running', answer: undefined, ms: undefined }), part('opencode', { status: 'failed', answer: undefined, error: { code: 'ask_timeout' } })],
  });
  const v = partViews(t, 1_000_000 + 12_400);
  expect(v.map((p) => [p.name, p.model, p.state, p.s])).toEqual([
    ['Claude', 'claude-std', 'done', 9],
    ['Codex', 'codex-std', 'answering', 12],
    ['OpenCode', 'opencode-std', 'failed', 2],
  ]);
  expect(v.map(rowValue)).toEqual([{ text: 'Answered, 9 s' }, { text: 'Answering, 12 s' }, { text: 'Did not answer' }]);
  expect(v[2].error).toBe('No answer within 3 minutes.');
  expect(v.map(aiStatus)).toEqual([
    { tone: 'sub', icon: 'check', text: '9 s' },
    { tone: 'waiting', icon: 'spinner', text: 'Answering…' },
    { tone: 'danger', text: 'Failed · No answer within 3 minutes.' },
  ]);
  expect(v.map(cardPreview)).toEqual([texts.claude, 'Answering…', 'No answer within 3 minutes.']);
  expect(answeredAgents(t)).toEqual(['claude']);

  // A run that could not be followed: the AIs still running did not answer; the turn says why.
  const lost = partViews(turn({ running: true, error: "Can't reach the PC.", parts: [part('claude'), part('codex', { status: 'running' })] }), 0);
  expect(lost.map((p) => [p.state, p.error])).toEqual([
    ['done', undefined],
    ['failed', 'Did not answer'],
    ['failed', 'Did not answer'],
  ]);
  expect(partError({ code: 'ask_failed', message: 'usage limit reached' })).toBe('The AI reported an error.\nusage limit reached');
  // #60: a signed-out AI says how to sign it in on the PC, not the CLI's own message.
  expect(partError({ code: 'ai_signed_out', message: 'claude auth login' })).toBe(
    "This AI isn't signed in on the PC. Run claude auth login in a terminal on the PC, then ask again.",
  );
  // R-118 (#62): a Mac agent started over SSH cannot read the sign-in; a hint without words of its own keeps the usual ones.
  expect(partError({ code: 'ai_signed_out', message: 'claude auth login', hint: 'mac_keychain' })).toBe(
    "This AI's sign-in can't be read: the PC's agent was started outside the Mac's own screen (over SSH, for example). On the Mac itself, open Terminal, run askorus-agent shutdown, then askorus-agent start, and ask again.",
  );
  expect(partError({ code: 'ai_signed_out', message: 'codex login', hint: 'some_newer_hint' })).toBe(
    "This AI isn't signed in on the PC. Run codex login in a terminal on the PC, then ask again.",
  );
  expect(partError({ code: 'ai_signed_out', message: 'opencode auth login' })).toBe(
    "This AI isn't signed in on the PC. Run opencode auth login in a terminal on the PC, then ask again.",
  );
});

// 11-18: OpenCode's free models need no sign-in but share a daily limit.
// The part says when they work again, in the phone's form, instead of
// OpenCode's English message.
test('free models at their limit say when they work again', () => {
  const now = new Date(2026, 10, 18, 2, 4);
  const today = new Date(2026, 10, 18, 9, 0);
  const tomorrow = new Date(2026, 10, 19, 9, 0);
  const hm = { hour: '2-digit', minute: '2-digit' } as const;
  expect(whenAgain(today.toISOString(), now)).toBe(today.toLocaleTimeString(undefined, hm));
  expect(whenAgain(tomorrow.toISOString(), now)).toBe(tomorrow.toLocaleString(undefined, { month: 'numeric', day: 'numeric', ...hm }));
  expect(whenAgain('', now)).toBe('');
  expect(whenAgain('soon', now)).toBe('');
  const at = new Date(Date.now() + 60 * 60 * 1000).toISOString();
  expect(partError({ code: 'ai_free_limit', message: at })).toBe(
    `OpenCode's free models reached their usage limit. They work again at ${whenAgain(at)}. To keep asking, run opencode auth login in a terminal on the PC and sign in to a provider (OpenCode Go, for example).`,
  );
  expect(partError({ code: 'ai_free_limit' })).toBe(
    "OpenCode's free models reached their usage limit for now. To keep asking, run opencode auth login in a terminal on the PC and sign in to a provider (OpenCode Go, for example).",
  );
  setLang('ko');
  expect(partError({ code: 'ai_free_limit', message: at })).toContain(`${whenAgain(at)}에 다시 쓸 수 있습니다.`);
  setLang('en');
});

test("a plan's usage limit (Antigravity's weekly limit, #20) says when it refreshes", () => {
  const at = '2026-11-25T07:21:31Z';
  expect(partError({ code: 'ai_usage_limit', message: at })).toBe(`This AI reached its plan's usage limit. It works again at ${whenAgain(at)}.`);
  expect(partError({ code: 'ai_usage_limit' })).toBe("This AI reached its plan's usage limit for now. Ask it again later.");
  setLang('ko');
  expect(partError({ code: 'ai_usage_limit', message: at })).toContain(`${whenAgain(at)}에 다시 쓸 수 있습니다.`);
  setLang('en');
});

test('Antigravity has its name, and is one of the moderators', () => {
  expect(agentName('agy')).toBe('Antigravity');
  expect(synthWriters).toContain('agy');
});

test('an AI still answering shows the model the turn asked for until the PC says which (UX-8)', () => {
  const going = { status: 'running', model: undefined, answer: undefined, ms: undefined } as const;
  const t = turn({ running: true, parts: [part('claude', going), part('codex', going), part('opencode', { model: 'opencode-go/deepseek-v4.1-flash#high' })] });
  // Claude with a model picked in the AI sheet, the others with the tier's.
  t.req = { ...t.req, agents: [{ agent: 'claude', model: 'claude-opus-5#max' }, { agent: 'codex' }, { agent: 'opencode' }] };
  const withTiers = info();
  withTiers.agents = withTiers.agents.map((a) => ({
    ...a,
    ask: { tiers: { quick: { model: `${a.agent}-quick` }, standard: { model: a.agent === 'codex' ? 'openai/gpt-6.1-terra#high' : `${a.agent}-std` } } },
  }));
  expect(partViews(t, 0, withTiers).map((p) => p.model)).toEqual(['claude-opus-5', 'gpt-6.1-terra', 'deepseek-v4.1-flash']);
  // Without the PC's info: the model asked for, else none yet.
  expect(partViews(t, 0).map((p) => p.model)).toEqual(['claude-opus-5', '', 'deepseek-v4.1-flash']);
  // Once the PC says which model answered, that one.
  const done = turn({ parts: [part('claude', { model: 'claude-sonnet-5' }), part('codex', { model: '' }), part('opencode')] });
  done.req = { ...done.req, tier: 'quick' };
  expect(partViews(done, 0, withTiers).map((p) => p.model)).toEqual(['claude-sonnet-5', 'codex-quick', 'opencode-std']);
});

test('times: the slowest answer, and the combined answer once it is written', () => {
  const parts = [part('claude', { ms: 9400 }), part('codex', { ms: 12_400 }), part('opencode', { status: 'failed', ms: 3000, answer: undefined })];
  expect(turnTimes(turn({ parts, snap: comparison({ synthesis: writing }) }))).toEqual({ answersMs: 12_400 });
  expect(metaLine(turn({ parts, snap: comparison({ synthesis: writing }) }))).toBe('answers 12 s');
  expect(turnTimes(turn({ parts, snap: comparison({ synthesis: written }) }))).toEqual({ answersMs: 12_400, synthMs: 15_200 });
  expect(metaLine(turn({ parts, snap: comparison({ synthesis: written }) }))).toBe('answers 12 s · combined in 15 s');
  // A snapshot of another comparison (a lost one) does not count.
  expect(metaLine(turn({ parts, compare: { id: 'cmp-2' }, snap: comparison({ synthesis: written }) }))).toBe('answers 12 s');
  setLang('ko');
  expect(metaLine(turn({ parts, snap: comparison({ synthesis: written }) }))).toBe('답 12초 · 종합 15초');
});

// ---- The combined answer's step ----

test('while the combined answer is coming its one step is the current one; written, the step goes', () => {
  // The comparison is being made (it ends at once since C6): the answer is coming.
  const t = turn({ snap: comparison({ running: true, synthesis: waiting }) });
  expect(steps(t, info())).toEqual([step]);
  expect(synthExpected(t)).toBe(true);
  expect(blockTitle(t, info())).toBe('Combined answer');
  // Final: waiting to be written, then being written.
  expect(steps(turn({ snap: comparison({ synthesis: waiting }) }), info())).toEqual([step]);
  expect(steps(turn({ snap: comparison({ synthesis: writing }) }), info())).toEqual([step]);
  // Written, or failed: no step.
  expect(steps(turn({ snap: comparison({ synthesis: written }) }), info())).toEqual([]);
  expect(blockTitle(turn({ snap: comparison({ synthesis: written }) }), info())).toBe('Combined answer');
  const failed: Synthesis = { status: 'failed', agent: 'claude', error: { code: 'judge_failed' } };
  expect(steps(turn({ snap: comparison({ synthesis: failed }) }), info())).toEqual([]);
  expect(blockTitle(turn({ snap: comparison({ synthesis: failed }) }), info())).toBe('Combined answer');
  setLang('ko');
  expect(steps(t, info())).toEqual([{ key: 'synth', label: '종합 답변 쓰는 중…' }]);
  expect(blockTitle(t, info())).toBe('종합 답변');
});

test('without a combined answer there is no step; before a snapshot the turn says what comes', () => {
  const off: Synthesis = { status: 'off' };
  const t = turn({ compare: { synth: { off: true } }, snap: comparison({ running: true, synthesis: off }) });
  expect(steps(t, info())).toEqual([]);
  expect(synthExpected(t)).toBe(false);
  expect(blockTitle(t, info())).toBe('Comparison');
  expect(steps(turn({ snap: comparison({ synthesis: off }) }), info())).toEqual([]);
  expect(blockTitle(turn({ snap: comparison({ synthesis: off }) }), info())).toBe('Comparison');
  // A comparison on its way (POST /v1/comparisons): the combined answer as asked.
  expect(steps(turn({ compare: { id: undefined, key: 'k1' } }), info())).toEqual([step]);
  expect(steps(turn({ compare: { id: undefined, key: 'k1', synth: { off: true } } }), info())).toEqual([]);
  // Before 비교하기 nothing is coming yet; a comparison the PC lost neither.
  expect(blockTitle(turn({ compare: { start: 'manual', id: undefined } }), info())).toBe('Comparison');
  expect(blockTitle(turn({ compare: { error: 'comparison_not_found' }, snap: comparison({ synthesis: waiting }) }), info())).toBe('Comparison');
  expect(steps(turn({ compare: { error: 'comparison_not_found' }, snap: comparison({ synthesis: waiting }) }), info())).toEqual([]);
  // An older agent: no synthesis at all.
  expect(steps(turn({ compare: { synth: undefined }, snap: comparison({ running: true }) }), info())).toEqual([]);
  expect(steps(turn({ compare: null }), info())).toEqual([]);
});
