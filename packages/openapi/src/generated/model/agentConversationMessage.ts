import type { AgentContextAttachment } from './agentContextAttachment';
import type { AgentConversationMessageRole } from './agentConversationMessageRole';

export interface AgentConversationMessage {
  /** @maxItems 64 */
  attachments: AgentContextAttachment[];
  createdAt: string;
  id: string;
  /** @pattern ^[1-9][0-9]*$ */
  ordinal: string;
  projectId: string;
  role: AgentConversationMessageRole;
  runId: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  text: string;
  turnId: string;
}
