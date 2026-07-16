import type { AgentBridgeTurn } from './agentBridgeTurn';

export interface AgentBridgeTurnPage {
  nextBefore?: string;
  projectId: string;
  runId: string;
  /** @maxItems 100 */
  turns: AgentBridgeTurn[];
}
