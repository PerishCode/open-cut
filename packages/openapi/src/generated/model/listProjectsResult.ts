import type { ProjectSummary } from './projectSummary';

export interface ListProjectsResult {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  activityCursor: string;
  /** @maxLength 512 */
  nextAfter?: string;
  /** @maxItems 100 */
  projects: ProjectSummary[];
}
