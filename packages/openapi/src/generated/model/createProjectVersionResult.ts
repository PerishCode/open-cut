import type { ProjectVersion } from './projectVersion';

export interface CreateProjectVersionResult {
  activityCursor: string;
  replayed: boolean;
  version: ProjectVersion;
}
