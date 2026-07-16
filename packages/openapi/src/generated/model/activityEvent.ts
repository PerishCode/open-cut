import type { ActivityActor } from './activityActor';
import type { ActivityOutcomeRef } from './activityOutcomeRef';
import type { ActivityScope } from './activityScope';
import type { ChangedEntityRef } from './changedEntityRef';

export interface ActivityEvent {
  actor?: ActivityActor;
  /** @maxItems 2048 */
  changedEntityRefs: ChangedEntityRef[];
  /** @pattern ^(0|[1-9][0-9]*)$ */
  cursor: string;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  eventId: string;
  kind: string;
  occurredAt: string;
  outcome?: ActivityOutcomeRef;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  projectId?: string;
  /** @pattern ^(0|[1-9][0-9]*)$ */
  projectRevision?: string;
  schema: string;
  scope: ActivityScope;
  summaryCode: string;
}
