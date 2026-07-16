
export type AgentPresentationEnvelopeKind = typeof AgentPresentationEnvelopeKind[keyof typeof AgentPresentationEnvelopeKind];


export const AgentPresentationEnvelopeKind = {
  'turn-started': 'turn-started',
  'context-rebuilt': 'context-rebuilt',
  'tool-started': 'tool-started',
  'tool-completed': 'tool-completed',
  'message-completed': 'message-completed',
  'turn-completed': 'turn-completed',
  'turn-failed': 'turn-failed',
} as const;
