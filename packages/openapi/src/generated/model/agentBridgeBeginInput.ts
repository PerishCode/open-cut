import type { AgentContextAttachment } from './agentContextAttachment';

export interface AgentBridgeBeginInput {
  /** @maxItems 64 */
  attachments: AgentContextAttachment[];
  /**
     * @minLength 1
     * @maxLength 32768
     */
  message: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
  sequenceId?: string;
}
