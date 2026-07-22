import type { ProjectVersion } from './projectVersion';

export interface RestoreProjectVersionResult {
  activityCursor: string;
  committedProjectRevision: string;
  replayed: boolean;
  safetyVersion: ProjectVersion;
  transactionId: string;
  version: ProjectVersion;
}
