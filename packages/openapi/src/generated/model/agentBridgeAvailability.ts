import type { AgentBridgeAvailabilityAdapterId } from './agentBridgeAvailabilityAdapterId';
import type { AgentBridgeAvailabilityPromptVersion } from './agentBridgeAvailabilityPromptVersion';
import type { AgentBridgeAvailabilityState } from './agentBridgeAvailabilityState';

export interface AgentBridgeAvailability {
  adapterId: AgentBridgeAvailabilityAdapterId;
  promptVersion: AgentBridgeAvailabilityPromptVersion;
  state: AgentBridgeAvailabilityState;
  /** @maxLength 128 */
  version?: string;
}
