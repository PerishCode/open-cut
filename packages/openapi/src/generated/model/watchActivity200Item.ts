import type { ActivityEvent } from './activityEvent';
import type { ActivityStreamReady } from './activityStreamReady';

export type WatchActivity200Item = {
  data: ActivityEvent;
  /** The event name. */
  event: 'activity';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
} | {
  data: ActivityStreamReady;
  /** The event name. */
  event: 'ready';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
};
