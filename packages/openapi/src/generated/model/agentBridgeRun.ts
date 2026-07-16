import type { AgentBridgeRunStatus } from './agentBridgeRunStatus';
import type { AgentBridgeTurn } from './agentBridgeTurn';

export interface AgentBridgeRun {
  activityCursor: string;
  agentId?: string;
  completedAt?: string;
  createdAt: string;
  currentTurn: AgentBridgeTurn;
  id: string;
  /**
     * @minLength 1
     * @maxLength 32768
     */
  intent: string;
  projectId: string;
  status: AgentBridgeRunStatus;
  updatedAt: string;
  /** @maxLength 128 */
  waitingReason?: string;
}
