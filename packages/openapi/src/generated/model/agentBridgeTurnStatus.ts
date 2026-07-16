
export type AgentBridgeTurnStatus = typeof AgentBridgeTurnStatus[keyof typeof AgentBridgeTurnStatus];


export const AgentBridgeTurnStatus = {
  starting: 'starting',
  active: 'active',
  detached: 'detached',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'cancelled',
  superseded: 'superseded',
} as const;
