import type { ProjectVersionRetention } from './projectVersionRetention';
import type { ProjectVersionSource } from './projectVersionSource';
import type { ProjectVersionTriggerKind } from './projectVersionTriggerKind';

export interface ProjectVersion {
  byteSize: string;
  /** @pattern ^[1-9][0-9]*$ */
  capturedProjectRevision: string;
  createdAt: string;
  digest: string;
  id: string;
  /** @maxLength 200 */
  name?: string;
  parentVersionId?: string;
  projectId: string;
  retention: ProjectVersionRetention;
  source: ProjectVersionSource;
  triggerId?: string;
  triggerKind?: ProjectVersionTriggerKind;
}
