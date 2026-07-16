import type { ActivityEvent } from './activityEvent';

export type WatchActivity200Item = {
  data: ActivityEvent;
  /** The event name. */
  event: 'activity';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
};
