import type { SourceExcerptTranscriptEvidence } from './sourceExcerptTranscriptEvidence';
import type { TimeRange } from './timeRange';

export interface SourceExcerptState {
  acceptedFingerprint: string;
  afterNodeId?: string;
  assetId: string;
  documentId: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  effectiveText: string;
  evidence: SourceExcerptTranscriptEvidence;
  id: string;
  /** @maxLength 64 */
  language: string;
  parentId: string;
  revision: string;
  sourceRange: TimeRange;
  tombstoned: boolean;
}
