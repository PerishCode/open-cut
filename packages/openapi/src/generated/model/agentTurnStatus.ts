
export type AgentTurnStatus = typeof AgentTurnStatus[keyof typeof AgentTurnStatus];


export const AgentTurnStatus = {
  starting: 'starting',
  active: 'active',
  detached: 'detached',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'cancelled',
  superseded: 'superseded',
} as const;
