
export type AgentBridgeRunStatus = typeof AgentBridgeRunStatus[keyof typeof AgentBridgeRunStatus];


export const AgentBridgeRunStatus = {
  authorizing: 'authorizing',
  active: 'active',
  waiting: 'waiting',
  paused: 'paused',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
