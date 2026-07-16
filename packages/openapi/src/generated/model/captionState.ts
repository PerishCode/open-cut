import type { CaptionProvenance } from './captionProvenance';
import type { CaptionProvenanceStatus } from './captionProvenanceStatus';
import type { TimeRange } from './timeRange';

export interface CaptionState {
  id: string;
  /** @maxLength 64 */
  language: string;
  provenance: CaptionProvenance;
  provenanceStatus?: CaptionProvenanceStatus;
  range: TimeRange;
  revision: string;
  sequenceId: string;
  /** @maxLength 262144 */
  text: string;
  tombstoned: boolean;
  trackId: string;
}
