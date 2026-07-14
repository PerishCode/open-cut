import type { Project } from './project';

export interface ProjectSnapshot {
  /**
     * Projects ordered by identifier
     * @nullable
     */
  projects: Project[] | null;
  /**
     * Monotonic project state revision
     * @minimum 0
     */
  revision: number;
}
