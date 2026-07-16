import type { AgentContextAttachmentKind } from './agentContextAttachmentKind';
import type { AgentContextEntityRef } from './agentContextEntityRef';
import type { AgentContextSequencePointRef } from './agentContextSequencePointRef';
import type { AgentContextSequenceRangeRef } from './agentContextSequenceRangeRef';
import type { AgentContextTranscriptRef } from './agentContextTranscriptRef';

export interface AgentContextAttachment {
  entity?: AgentContextEntityRef;
  kind: AgentContextAttachmentKind;
  point?: AgentContextSequencePointRef;
  range?: AgentContextSequenceRangeRef;
  transcript?: AgentContextTranscriptRef;
}
