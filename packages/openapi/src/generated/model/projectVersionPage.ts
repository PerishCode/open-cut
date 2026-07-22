import type { ProjectVersion } from './projectVersion';

export interface ProjectVersionPage {
  activityCursor: string;
  nextBefore?: string;
  /** @maxItems 50 */
  versions: ProjectVersion[];
}
