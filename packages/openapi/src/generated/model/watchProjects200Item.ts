import type { ProjectSnapshot } from './projectSnapshot';
import type { ProjectUpserted } from './projectUpserted';

export type WatchProjects200Item = {
  data: ProjectSnapshot;
  /** The event name. */
  event: 'project.snapshot';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
} | {
  data: ProjectUpserted;
  /** The event name. */
  event: 'project.upserted';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
};
