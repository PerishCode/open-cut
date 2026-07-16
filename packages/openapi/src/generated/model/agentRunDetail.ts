import type { ActorRef } from './actorRef';
import type { AgentRunDetailStatus } from './agentRunDetailStatus';
import type { AgentTurn } from './agentTurn';

export interface AgentRunDetail {
  activityCursor: string;
  actor: ActorRef;
  completedAt?: string;
  createdAt: string;
  currentTurn: AgentTurn;
  id: string;
  /** @maxLength 32768 */
  intent: string;
  latestObservedProjectRevision: string;
  projectId: string;
  startedProjectRevision: string;
  status: AgentRunDetailStatus;
  updatedAt: string;
  /** @maxLength 128 */
  waitingReason?: string;
}
