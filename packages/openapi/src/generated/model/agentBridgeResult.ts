import type { AgentBridgeRun } from './agentBridgeRun';
import type { AgentConversationMessage } from './agentConversationMessage';

export interface AgentBridgeResult {
  message?: AgentConversationMessage;
  replayed: boolean;
  run: AgentBridgeRun;
}
