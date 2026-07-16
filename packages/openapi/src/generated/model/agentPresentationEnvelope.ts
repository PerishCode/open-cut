import type { AgentPresentationEnvelopeKind } from './agentPresentationEnvelopeKind';
import type { AgentPresentationEnvelopeTool } from './agentPresentationEnvelopeTool';

export interface AgentPresentationEnvelope {
  kind: AgentPresentationEnvelopeKind;
  runId: string;
  /** @pattern ^[1-9][0-9]*$ */
  sequence: string;
  tool?: AgentPresentationEnvelopeTool;
  turnId: string;
}
