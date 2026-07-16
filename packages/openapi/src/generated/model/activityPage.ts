import type { ActivityEvent } from './activityEvent';

export interface ActivityPage {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  cursor: string;
  /** @maxItems 500 */
  events: ActivityEvent[];
  hasMore: boolean;
}
