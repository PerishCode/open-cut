
export type AgentRunDetailStatus = typeof AgentRunDetailStatus[keyof typeof AgentRunDetailStatus];


export const AgentRunDetailStatus = {
  authorizing: 'authorizing',
  active: 'active',
  waiting: 'waiting',
  paused: 'paused',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
