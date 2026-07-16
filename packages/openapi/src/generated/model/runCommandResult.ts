import type { AgentRunDetail } from './agentRunDetail';

export interface RunCommandResult {
  replayed: boolean;
  run: AgentRunDetail;
}
