// Display names of the AIs the agent may report. They are product names,
// the same in every language.

const names: Record<string, string> = { claude: 'Claude', codex: 'Codex', opencode: 'OpenCode' };

/** agentName is the name to show for an agent code; unknown codes show as they are. */
export function agentName(agent: string): string {
  return names[agent] ?? agent;
}
