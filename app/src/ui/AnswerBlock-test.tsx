// R-61 (#4): what an AI adds at the end of an answer (OpenCode's '다음에
// 쓸 문장' from the person's own rules) is folded under the answer, as the
// comparison and the combined answer leave it out.

import { render, screen, userEvent } from '@testing-library/react-native';

import type { PartView } from '@/ask/present';

import { AnswerBlock } from './AnswerBlock';

const view = (answer: string): PartView => ({
  agent: 'opencode',
  name: 'OpenCode',
  model: 'gpt-5.4',
  state: 'done',
  s: 9.2,
  part: { answer, ms: 9200 } as unknown as PartView['part'],
});

test('the extras fold under the answer and open on a tap', async () => {
  const user = userEvent.setup();
  await render(<AnswerBlock p={view('헤드셋 유격을 먼저 보세요.\n\n**다음에 쓸 문장**\n```\n포크 휨 확인하는 법 알려줘\n```\n')} />);
  expect(screen.getByText('헤드셋 유격을 먼저 보세요.')).toBeTruthy();
  expect(screen.queryByText('다음에 쓸 문장')).toBeNull();
  expect(screen.queryByText(/포크 휨/)).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Show what was added' }));
  expect(screen.getByText('Added at the end of the answer, so the comparison and the combined answer leave it out.')).toBeTruthy();
  expect(screen.getByText(/포크 휨 확인하는 법 알려줘/)).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Hide what was added' }));
  expect(screen.queryByText(/포크 휨/)).toBeNull();
});

test('an answer without extras has no fold', async () => {
  await render(<AnswerBlock p={view('헤드셋 유격을 먼저 보세요.')} />);
  expect(screen.getByText('헤드셋 유격을 먼저 보세요.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Show what was added' })).toBeNull();
});
