
export type AgentPresentationEnvelopeTool = typeof AgentPresentationEnvelopeTool[keyof typeof AgentPresentationEnvelopeTool];


export const AgentPresentationEnvelopeTool = {
  command: 'command',
  'file-change': 'file-change',
  reasoning: 'reasoning',
  'web-search': 'web-search',
  plan: 'plan',
} as const;
