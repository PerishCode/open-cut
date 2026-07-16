import type { ActivityActorKind } from './activityActorKind';

export interface ActivityActor {
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  id: string;
  kind: ActivityActorKind;
}
