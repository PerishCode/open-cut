import type { AgentContextAttachment } from './agentContextAttachment';

export interface AgentBridgeContinueInput {
  /** @maxItems 64 */
  attachments: AgentContextAttachment[];
  /** @pattern ^[1-9][0-9]*$ */
  expectedGeneration: string;
  /**
     * @minLength 1
     * @maxLength 32768
     */
  message: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
  sequenceId?: string;
}
