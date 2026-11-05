// S5c: the shared pieces the rebuilt screens are made of, each as a person
// (or a screen reader) meets it.

import { act, render, screen, userEvent, within } from '@testing-library/react-native';
import * as SystemUI from 'expo-system-ui';
import { Keyboard, Platform, StyleSheet, Text } from 'react-native';
import { SafeAreaInsetsContext } from 'react-native-safe-area-context';

import type { KeyValue } from '@/connections/store';
import { AICardStrip } from '@/ui/AICardStrip';
import { AISheet } from '@/ui/AISheet';
import { AvatarPile, initials } from '@/ui/Avatar';
import { Button } from '@/ui/Button';
import { CombinedAnswerCard } from '@/ui/CombinedAnswer';
import { Composer, FollowBar } from '@/ui/Composer';
import { Chip, Mark, PillChoice, Segment, SwitchRow } from '@/ui/controls';
import { Icon, IconButton, iconNames } from '@/ui/Icon';
import { Group, Row } from '@/ui/List';
import { RefineLine } from '@/ui/Refine';
import { Sheet } from '@/ui/Sheet';
import { aiPalettes, palettes, ThemeProvider, useAIColours, useTheme, useThemeChoice } from '@/ui/theme';
import { ThreadBubble } from '@/ui/ThreadBubble';

type HostNode = { type: string; parent: HostNode | null; children: Array<HostNode | string> };
/** inText: whether an element sits inside a Text (on Android a nested Text is part of that Text's node). */
const inText = (el: HostNode) => {
  for (let p = el.parent; p; p = p.parent) if (p.type === 'Text') return true;
  return false;
};
/** textOf: the text an element draws, in order. */
const textOf = (el: HostNode | string): string => (typeof el === 'string' ? el : el.children.map(textOf).join(''));

afterEach(() => jest.restoreAllMocks());

describe('avatars', () => {
  test('two letters per AI', () => {
    expect(['claude', 'codex', 'opencode', 'gemini', 'antigravity', 'kimi'].map(initials)).toEqual(['Cl', 'Cx', 'Oc', 'Ag', 'Ag', 'Ki']);
    expect(initials('mistral')).toBe('Mi');
    expect(initials('x-ai')).toBe('Xa');
    expect(initials('')).toBe('?');
  });

  test('a pile shows four, then +n; it is decoration', async () => {
    await render(<AvatarPile agents={['claude', 'codex', 'gemini', 'opencode', 'kimi']} ring="#151618" />);
    const shown = { includeHiddenElements: true };
    expect(['Cl', 'Cx', 'Ag', 'Oc', '+1'].map((x) => screen.getAllByText(x, shown).length)).toEqual([1, 1, 1, 1, 1]);
    expect(screen.queryByText('Ki', shown)).toBeNull();
    expect(screen.queryByText('Cl')).toBeNull(); // hidden from screen readers

    await render(<AvatarPile agents={['claude', 'codex', 'opencode']} ring="#151618" />);
    expect(screen.queryByText(/^\+/, shown)).toBeNull();
  });

  // #6: the circle's fill (the nearest fill around the letters) and the letters' colour.
  const look = (letters: string) => {
    const el = screen.getByText(letters, { includeHiddenElements: true });
    let fill: string | undefined;
    for (let p = el.parent; p && !fill; p = p.parent) fill = StyleSheet.flatten(p.props.style)?.backgroundColor;
    return { bg: fill, fg: StyleSheet.flatten(el.props.style).color };
  };

  test('#6: each AI we know has its own colour; a dim one stays an outline and others stay plain', async () => {
    const light = aiPalettes.light;
    await render(<AvatarPile agents={['claude', 'codex', 'opencode', 'mistral']} dim={['opencode']} ring="#FCFAF8" />);
    expect(['Cl', 'Cx', 'Oc', 'Mi'].map(look)).toEqual([
      light.sky,
      light.lilac,
      { bg: '#FCFAF8', fg: palettes.light.caption },
      { bg: palettes.light.avatar, fg: palettes.light.text },
    ]);
    // Five AIs, five colours.
    const all = ['sky', 'lilac', 'teal', 'rose', 'lime'] as const;
    expect(new Set(all.map((h) => aiPalettes.dark[h].bg)).size).toBe(5);
    expect(new Set(all.map((h) => aiPalettes.light[h].bg)).size).toBe(5);
  });

  test('#6: with AI colours off in Settings every avatar is the one plain colour again', async () => {
    const kv: KeyValue = { get: async () => '{"theme":"dark","aiColours":false}', set: async () => {}, remove: async () => {} };
    await render(
      <ThemeProvider kv={kv}>
        <AvatarPile agents={['claude', 'codex']} ring="#151618" />
      </ThemeProvider>,
    );
    expect(['Cl', 'Cx'].map(look)).toEqual([0, 1].map(() => ({ bg: palettes.dark.avatar, fg: palettes.dark.text })));
  });
});

// Milestone C: the line under a question while the moderator reads it,
// and nothing for a run that did not refine.
describe('refine line', () => {
  test('refining, then nothing without a refine', async () => {
    const onOpen = jest.fn();
    await render(<RefineLine refine={{ status: 'running' }} onOpen={onOpen} />);
    expect(screen.getByText('Refining the question…')).toBeTruthy();
    await render(<RefineLine refine={undefined} onOpen={onOpen} />);
    expect(screen.toJSON()).toBeNull();
    await render(<RefineLine refine={{ status: 'done', spec: { kind: 'poem', intent: 'x', depth: 'deep', language: 'en' }, template: '' }} onOpen={onOpen} />);
    expect(screen.getByRole('button', { name: 'Answer format: poem. Show' })).toBeTruthy(); // an unknown kind as the PC wrote it
  });
});

describe('controls', () => {
  test('a segment is a radio group with the picked one checked', async () => {
    const user = userEvent.setup();
    const onChange = jest.fn();
    await render(
      <Segment options={['quick', 'standard', 'deep'] as const} value="standard" onChange={onChange} labelOf={(x) => x.toUpperCase()} look="tier" label="Tier" />,
    );
    expect(screen.getByLabelText('Tier').props.accessibilityRole).toBe('radiogroup');
    expect(screen.getByRole('radio', { name: 'STANDARD' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'DEEP' })).not.toBeChecked();
    await user.press(screen.getByRole('radio', { name: 'DEEP' }));
    expect(onChange).toHaveBeenCalledWith('deep');

    await render(<Segment options={['system', 'dark']} value="dark" onChange={onChange} labelOf={(x) => x} look="setting" label="Theme" />);
    expect(screen.getByRole('radio', { name: 'dark' })).toBeChecked();
  });

  test('a chip says whether it is picked', async () => {
    const user = userEvent.setup();
    const onPress = jest.fn();
    await render(
      <>
        <Chip label="All" selected onPress={onPress} />
        <Chip label="Running" selected={false} onPress={onPress} />
      </>,
    );
    expect(screen.getByRole('button', { name: 'All' })).toBeSelected();
    expect(screen.getByRole('button', { name: 'Running' })).not.toBeSelected();
    await user.press(screen.getByRole('button', { name: 'Running' }));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  test('a switch row is a switch', async () => {
    const user = userEvent.setup();
    const onChange = jest.fn();
    await render(<SwitchRow label="Compare answers automatically" value onChange={onChange} />);
    expect(screen.getByRole('switch', { name: 'Compare answers automatically' })).toBeChecked();
    await user.press(screen.getByRole('switch', { name: 'Compare answers automatically' }));
    expect(onChange).toHaveBeenCalledWith(false);
  });

  test('pills pick one, with avatars', async () => {
    const user = userEvent.setup();
    const onChange = jest.fn();
    await render(
      <PillChoice
        label="Writer"
        value="claude"
        onChange={onChange}
        options={[
          { value: 'claude', label: 'Claude', agent: 'claude' },
          { value: 'codex', label: 'Codex', agent: 'codex' },
        ]}
      />,
    );
    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked();
    await user.press(screen.getByRole('radio', { name: 'Codex' }));
    expect(onChange).toHaveBeenCalledWith('codex');
  });

  test('a mark shows its count', async () => {
    await render(<Mark n={3} tone="fill" size="lg" />);
    expect(screen.getByText('3')).toBeTruthy();
  });
});

describe('buttons and icons', () => {
  test('the old props still work: secondary is outline, danger says it in red', async () => {
    const user = userEvent.setup();
    const onPress = jest.fn();
    await render(
      <>
        <Button label="Pair" onPress={onPress} />
        <Button secondary label="Cancel" onPress={onPress} />
        <Button danger label="Unpair" onPress={onPress} />
        <Button variant="quiet" icon="copy" label="Copy" onPress={onPress} />
        <Button label="Start" onPress={onPress} disabled />
        <Button label="Starting" onPress={onPress} busy />
      </>,
    );
    for (const name of ['Pair', 'Cancel', 'Unpair', 'Copy']) await user.press(screen.getByRole('button', { name }));
    expect(onPress).toHaveBeenCalledTimes(4);
    expect(screen.getByRole('button', { name: 'Start' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Starting' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Starting' })).toBeBusy();
    const dangerText = screen.getByText('Unpair');
    const plainText = screen.getByText('Cancel');
    const colour = (el: typeof dangerText) => [el.props.style].flat(3).reduce((c, x) => (x && x.color ? x.color : c), '');
    expect(colour(dangerText)).not.toBe(colour(plainText));
  });

  test('every icon has a picture, and an icon button is named', async () => {
    const user = userEvent.setup();
    expect(iconNames).toHaveLength(31);
    await render(
      <>
        {iconNames.map((n) => (
          <Icon key={n} name={n} color="#FFFFFF" />
        ))}
      </>,
    );
    for (const n of iconNames) expect(screen.getByTestId(`icon-${n}`).props.source).toBeTruthy();
    const onPress = jest.fn();
    await render(<IconButton name="menu" label="Open menu" onPress={onPress} />);
    await user.press(screen.getByRole('button', { name: 'Open menu' }));
    expect(onPress).toHaveBeenCalled();
  });
});

describe('lists and sheets', () => {
  test('a row always has an accessibility value, an empty one when it has none (R-40)', async () => {
    await render(
      <Group>
        <Row title="Asksemble 0.1.0" subtitle="Not an official product of any AI company." onPress={() => undefined} chevron />
        <Row title="Writer" accessibilityLabel="Writer" accessibilityValue={{ text: 'Claude' }} />
      </Group>,
    );
    expect(screen.getByRole('button', { name: 'Asksemble 0.1.0, Not an official product of any AI company.' }).props.accessibilityValue).toEqual({ text: '' });
    expect(screen.getByLabelText('Writer').props.accessibilityValue).toEqual({ text: 'Claude' });
  });

  test('a sheet closes from its button and from the dimmed screen', async () => {
    const user = userEvent.setup();
    const onClose = jest.fn();
    await render(
      <Sheet open onClose={onClose} title="Answer format sent" subtitle="answers 12 s">
        <Text>inside</Text>
      </Sheet>,
    );
    expect(screen.getByText('Answer format sent')).toBeTruthy();
    expect(screen.getByText('answers 12 s')).toBeTruthy();
    expect(screen.getByText('inside')).toBeTruthy();
    // The dimmed screen is for a touch; a screen reader gets the one Close button.
    await user.press(screen.getByTestId('sheet-scrim', { includeHiddenElements: true }));
    expect(onClose).toHaveBeenCalledTimes(1);
    await user.press(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(2);

    await render(
      <Sheet open={false} onClose={onClose}>
        <Text>gone</Text>
      </Sheet>,
    );
    expect(screen.queryByText('gone')).toBeNull();
  });

  test('the AI sheet lists each AI with its state, values always objects', async () => {
    const user = userEvent.setup();
    const open = jest.fn();
    await render(
      <AISheet
        open
        onClose={() => undefined}
        title="3 AIs"
        subtitle="Collecting answers · 10 s"
        rows={[
          { agent: 'claude', name: 'Claude', model: 'sonnet', status: { tone: 'sub', text: '9 s', icon: 'check' }, onPress: open, accessibilityValue: { text: 'Answered, 9 s' } },
          { agent: 'codex', name: 'Codex', status: { tone: 'waiting', text: 'Answering', icon: 'spinner' }, accessibilityValue: { text: '' } },
          { agent: 'kimi', name: 'Kimi', status: { tone: 'danger', text: 'Failed · sign in' }, disabled: true, onPress: open, accessibilityValue: { text: '' } },
        ]}
      />,
    );
    const claude = screen.getByRole('button', { name: 'Claude, sonnet' });
    expect(claude.props.accessibilityValue).toEqual({ text: 'Answered, 9 s' });
    await user.press(claude);
    expect(open).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText('Codex, Answering').props.accessibilityValue).toEqual({ text: '' });
    expect(screen.getByRole('button', { name: 'Kimi, Failed · sign in' })).toBeDisabled();
  });

  test('the question shows as a bubble', async () => {
    await render(<ThreadBubble text="전기차 배터리는 보통 몇 년 쓸 수 있어?" />);
    expect(screen.getByText('전기차 배터리는 보통 몇 년 쓸 수 있어?')).toBeTruthy();
  });
});

describe('writing a question', () => {
  test('the composer names its parts and lifts over the keyboard', async () => {
    const listeners: Record<string, (e: any) => void> = {};
    jest.spyOn(Keyboard, 'addListener').mockImplementation(((name: string, cb: (e: any) => void) => {
      listeners[name] = cb;
      return { remove: () => undefined } as any;
    }) as any);
    const user = userEvent.setup();
    const props = { onSend: jest.fn(), onPickAIs: jest.fn(), onPickTier: jest.fn(), onChangeText: jest.fn() };
    const view = (value: string) => (
      <Composer
        {...props}
        value={value}
        canSend={!!value}
        placeholder="Ask a question"
        agents={['claude', 'codex', 'opencode']}
        pickLabel="AIs to answer: 3 (Claude, Codex, OpenCode). Pick"
        tier="standard"
      />
    );
    await render(view(''));
    expect(screen.getByRole('button', { name: 'Ask' })).toBeDisabled();
    await user.type(screen.getByLabelText('Ask a question'), 'Hi');
    expect(props.onChangeText).toHaveBeenCalled();
    await user.press(screen.getByRole('button', { name: 'AIs to answer: 3 (Claude, Codex, OpenCode). Pick' }));
    expect(props.onPickAIs).toHaveBeenCalled();
    // The tier is one chip with the picked one (#16): it opens the tier sheet.
    expect(screen.queryByRole('radio')).toBeNull();
    await user.press(screen.getByRole('button', { name: 'Tier: Standard. Pick' }));
    expect(props.onPickTier).toHaveBeenCalled();

    await render(view('Hi'));
    await user.press(screen.getByRole('button', { name: 'Ask' }));
    expect(props.onSend).toHaveBeenCalledTimes(1);

    // iOS tells before the keyboard moves, Android after.
    const [shown, hidden] = Platform.OS === 'ios' ? ['keyboardWillShow', 'keyboardWillHide'] : ['keyboardDidShow', 'keyboardDidHide'];
    const bottom = () => StyleSheet.flatten(screen.getByTestId('composer').props.style).marginBottom;
    expect(bottom()).toBe(12);
    await act(async () => listeners[shown]({ endCoordinates: { height: 300, screenY: 500 } }));
    expect(bottom()).toBe(312);
    await act(async () => listeners[hidden]({}));
    expect(bottom()).toBe(12);
  });

  test('on Android the bar also clears the navigation bar under the keyboard', async () => {
    const listeners: Record<string, (e: any) => void> = {};
    jest.spyOn(Keyboard, 'addListener').mockImplementation(((name: string, cb: (e: any) => void) => {
      listeners[name] = cb;
      return { remove: () => undefined } as any;
    }) as any);
    jest.replaceProperty(Platform, 'OS', 'android');
    const insets = { top: 24, right: 0, bottom: 20, left: 0 };
    await render(
      <SafeAreaInsetsContext.Provider value={insets}>
        <FollowBar value="" onChangeText={() => undefined} onSend={() => undefined} canSend placeholder="Follow up" sendLabel="Send" />
      </SafeAreaInsetsContext.Provider>,
    );
    const bottom = () => StyleSheet.flatten(screen.getByTestId('follow-bar').props.style).paddingBottom;
    expect(bottom()).toBe(6 + 20); // above the navigation bar
    // Android's keyboard height leaves out the navigation bar under it (RN ReactRootView).
    await act(async () => listeners.keyboardDidShow({ endCoordinates: { height: 300, screenY: 500 } }));
    expect(bottom()).toBe(6 + 300 + 20);
    await act(async () => listeners.keyboardDidHide({}));
    expect(bottom()).toBe(6 + 20);
  });

  test('a follow-up to one AI shows the chip, which cancels', async () => {
    const user = userEvent.setup();
    const onCancel = jest.fn();
    const onSend = jest.fn();
    await render(
      <FollowBar
        value=""
        onChangeText={() => undefined}
        onSend={onSend}
        canSend={false}
        placeholder="Ask Codex a follow-up"
        sendLabel="Send to Codex"
        sendHint="You can send once every answer is in."
        to={{ agent: 'codex', label: 'Only Codex', cancelLabel: 'Stop asking only Codex', onCancel }}
      />,
    );
    expect(screen.getByLabelText('Ask Codex a follow-up')).toBeTruthy();
    expect(screen.getByText('Only Codex')).toBeTruthy();
    const send = screen.getByRole('button', { name: 'Send to Codex' });
    expect(send).toBeDisabled();
    expect(send.props.accessibilityHint).toBe('You can send once every answer is in.');
    await user.press(screen.getByRole('button', { name: 'Stop asking only Codex' }));
    expect(onCancel).toHaveBeenCalled();
  });
});

describe('the combined answer', () => {
  // R-70 (11-05, S10): 'answers 11 s · combined in 10 s' was cut to
  // 'answers 11 s · combine…' beside the title. It may take two lines now,
  // and only breaks after a ' · '.
  test('the times take two lines rather than being cut, and a part never splits', async () => {
    await render(
      <CombinedAnswerCard
        pile={{ agents: ['claude', 'codex'], label: 'AIs that answered: 2 (Claude, Codex). Details', onPress: () => undefined }}
        title="Combined answer"
        meta="answers 11 s · combined in 10 s"
      />,
    );
    const meta = screen.getByText('answers 11 s · combined in 10 s');
    expect(meta.props.numberOfLines).toBe(2);
    expect(textOf(meta as unknown as HostNode)).toBe('answers 11 s · combined in 10 s');
  });

  test('while it is written: its one step, read aloud, over grey lines', async () => {
    await render(
      <CombinedAnswerCard
        pile={{ agents: ['claude', 'codex', 'opencode'], label: 'AIs that answered: 3 (Claude, Codex, OpenCode). Details', onPress: () => undefined }}
        title="Combined answer"
        meta="answers 12 s"
        steps={[{ key: 'synth', label: 'Writing the combined answer…' }]}
        skeleton
      />,
    );
    expect(screen.getByLabelText('Writing the combined answer…')).toBeTruthy();
    expect(screen.getByText('answers 12 s')).toBeTruthy();
    expect(screen.getByTestId('skeleton', { includeHiddenElements: true })).toBeTruthy();
    // The title row has the pile alone to press (no details button since C6d).
    const head = screen.getByRole('header', { name: 'Combined answer' }).parent!;
    expect(within(head).getAllByRole('button').map((b) => b.props.accessibilityLabel)).toEqual(['AIs that answered: 3 (Claude, Codex, OpenCode). Details']);
  });

  test('done: the body and the footnote, no step or grey lines', async () => {
    await render(
      <CombinedAnswerCard
        pile={{ agents: ['claude', 'codex'], label: 'AIs', onPress: () => undefined }}
        title="Combined answer"
        footnote="An AI wrote this combined answer, so it can be wrong."
      >
        <Text>body</Text>
      </CombinedAnswerCard>,
    );
    expect(screen.getByText('body')).toBeTruthy();
    expect(screen.getByText('An AI wrote this combined answer, so it can be wrong.')).toBeTruthy();
    expect(screen.queryByLabelText('Writing the combined answer…')).toBeNull();
    expect(screen.queryByTestId('skeleton', { includeHiddenElements: true })).toBeNull();
  });

  test('AI cards: the one a follow-up goes to says so', async () => {
    const user = userEvent.setup();
    const open = jest.fn();
    await render(
      <AICardStrip
        title="Answers by AI"
        count="3"
        cards={[
          { key: 'claude', agent: 'claude', name: 'Claude', model: 'sonnet', preview: '15~20년', state: 'done', a11yLabel: "Claude's answer", onPress: open },
          { key: 'codex', agent: 'codex', name: 'Codex', model: 'gpt', preview: '10~15년', state: 'done', selected: true, selectedLabel: 'Only this AI', a11yLabel: "Codex's answer", onPress: open },
          { key: 'kimi', agent: 'kimi', name: 'Kimi', preview: 'Failed', state: 'failed', a11yLabel: "Kimi's answer", onPress: open },
        ]}
      />,
    );
    expect(screen.getByRole('button', { name: "Codex's answer" })).toBeSelected();
    expect(screen.getByText('Only this AI')).toBeTruthy();
    expect(screen.queryByText('gpt')).toBeNull(); // the selected label stands in for the model
    expect(screen.getByText('sonnet')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: "Claude's answer" }));
    expect(open).toHaveBeenCalled();
  });
});

describe('theme provider', () => {
  const memory = (saved?: string) => {
    const mem = new Map<string, string>(saved ? [['ui.v1', saved]] : []);
    const kv: KeyValue = { get: async (k) => mem.get(k) ?? null, set: async (k, v) => void mem.set(k, v), remove: async () => {} };
    return { mem, kv };
  };

  function Probe() {
    const th = useTheme();
    const { choice, setChoice, ready } = useThemeChoice();
    return (
      <>
        <Text testID="probe">{`${th.scheme} ${th.bg} ${choice} ${ready ? 'ready' : 'reading'}`}</Text>
        <Text onPress={() => setChoice('light')}>light</Text>
      </>
    );
  }

  test('reads the saved choice, draws it, saves a new one and sets the window colour', async () => {
    const user = userEvent.setup();
    const { mem, kv } = memory('{"theme":"dark"}');
    await render(
      <ThemeProvider kv={kv}>
        <Probe />
      </ThemeProvider>,
    );
    expect(screen.getByTestId('probe')).toHaveTextContent('dark #151618 dark ready');
    expect(SystemUI.setBackgroundColorAsync).toHaveBeenLastCalledWith('#151618');
    await user.press(screen.getByText('light'));
    expect(screen.getByTestId('probe')).toHaveTextContent('light #FCFAF8 light ready');
    expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'light', aiColours: true });
    expect(SystemUI.setBackgroundColorAsync).toHaveBeenLastCalledWith('#FCFAF8');
  });

  test('#6: AI colours are read and saved with the theme; a change made while reading wins for that setting only', async () => {
    const user = userEvent.setup();
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    const { mem } = memory('{"theme":"dark","aiColours":true}');
    const kv: KeyValue = { get: async (k) => (await gate, mem.get(k) ?? null), set: async (k, v) => void mem.set(k, v), remove: async () => {} };
    function Colours() {
      const { on, setOn } = useAIColours();
      return <Text onPress={() => setOn(!on)} testID="colours">{on ? 'on' : 'off'}</Text>;
    }
    await render(
      <ThemeProvider kv={kv}>
        <Probe />
        <Colours />
      </ThemeProvider>,
    );
    expect(screen.getByTestId('colours')).toHaveTextContent('on');
    await user.press(screen.getByTestId('colours'));
    expect(screen.getByTestId('colours')).toHaveTextContent('off');
    expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'dark', aiColours: true }); // nothing saved before reading

    await act(async () => release());
    expect(screen.getByTestId('probe')).toHaveTextContent('dark #151618 dark ready');
    expect(screen.getByTestId('colours')).toHaveTextContent('off');
    expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'dark', aiColours: false });

    await user.press(screen.getByTestId('colours'));
    expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'dark', aiColours: true });
  });

  test('outside a provider the phone’s scheme is used and the choice does nothing', async () => {
    await render(<Probe />);
    expect(screen.getByTestId('probe')).toHaveTextContent(/^(light #FCFAF8|dark #151618) system ready$/);
  });
});
