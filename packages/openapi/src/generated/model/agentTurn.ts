import type { AgentTurnStatus } from './agentTurnStatus';

export interface AgentTurn {
  endedAt?: string;
  /** @pattern ^[1-9][0-9]*$ */
  generation: string;
  id: string;
  projectId: string;
  runId: string;
  startedAt: string;
  status: AgentTurnStatus;
}
