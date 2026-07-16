import type { TrackSummaryType } from './trackSummaryType';

export interface TrackSummary {
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  id: string;
  label: string;
  /** @pattern ^(0|[1-9][0-9]*)$ */
  revision: string;
  type: TrackSummaryType;
}
