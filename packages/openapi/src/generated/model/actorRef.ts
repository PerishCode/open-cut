import type { ActorRefKind } from './actorRefKind';

export interface ActorRef {
  agentId?: string;
  creatorId?: string;
  kind: ActorRefKind;
}
