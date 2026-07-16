import type { AgentConversationMessage } from './agentConversationMessage';

export interface AgentConversationPage {
  /** @maxItems 100 */
  messages: AgentConversationMessage[];
  nextAfter?: string;
  projectId: string;
  runId: string;
}
