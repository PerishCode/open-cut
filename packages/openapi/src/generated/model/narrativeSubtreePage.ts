import type { NarrativeNodeState } from './narrativeNodeState';
import type { NarrativeSectionSummary } from './narrativeSectionSummary';

export interface NarrativeSubtreePage {
  activityCursor: string;
  documentId: string;
  documentRevision: string;
  nextAfter?: string;
  /** @maxItems 200 */
  nodes: NarrativeNodeState[];
  parent: NarrativeSectionSummary;
}
