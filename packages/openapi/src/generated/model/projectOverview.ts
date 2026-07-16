import type { ProjectSummary } from './projectSummary';
import type { SequenceFormat } from './sequenceFormat';
import type { TrackSummary } from './trackSummary';

export interface ProjectOverview {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  activityCursor: string;
  format: SequenceFormat;
  /** @pattern ^[1-9][0-9]*$ */
  mainSequenceRevision: string;
  /** @pattern ^[1-9][0-9]*$ */
  narrativeDocumentRevision: string;
  narrativeRootNodeId: string;
  project: ProjectSummary;
  /** @maxItems 64 */
  tracks: TrackSummary[];
}
