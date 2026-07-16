import type { AgentBridgeRun } from './agentBridgeRun';

export interface AgentBridgeRunPage {
  projectId: string;
  /** @maxItems 20 */
  runs: AgentBridgeRun[];
}
