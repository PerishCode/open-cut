import type { Project } from './project';

export interface ProjectUpserted {
  project: Project;
  /**
     * Monotonic project state revision
     * @minimum 0
     */
  revision: number;
}
