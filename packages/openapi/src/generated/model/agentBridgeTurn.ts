import type { AgentBridgeTurnStatus } from './agentBridgeTurnStatus';

export interface AgentBridgeTurn {
  endedAt?: string;
  /** @pattern ^[1-9][0-9]*$ */
  generation: string;
  id: string;
  sequenceId?: string;
  startedAt: string;
  status: AgentBridgeTurnStatus;
}
