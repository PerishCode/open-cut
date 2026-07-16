import type { EntityRevisionChangeKind } from './entityRevisionChangeKind';

export interface EntityRevisionChange {
  after: string;
  before?: string;
  id: string;
  kind: EntityRevisionChangeKind;
  tombstoned?: boolean;
}
