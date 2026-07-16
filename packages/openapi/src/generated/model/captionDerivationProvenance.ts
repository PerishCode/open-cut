import type { CaptionDerivationPolicy } from './captionDerivationPolicy';
import type { TimeRange } from './timeRange';
import type { TranscriptCorrectionRevisionRef } from './transcriptCorrectionRevisionRef';

export interface CaptionDerivationProvenance {
  acceptedFingerprint: string;
  assetId: string;
  clipId: string;
  clipRevision: string;
  clipSourceRange: TimeRange;
  clipTimelineRange: TimeRange;
  /** @maxItems 256 */
  correctionRevisions: TranscriptCorrectionRevisionRef[];
  /** @maxLength 64 */
  derivedLanguage: string;
  derivedRange: TimeRange;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  derivedText: string;
  evidenceSourceRange: TimeRange;
  policy: CaptionDerivationPolicy;
  /**
     * @minItems 1
     * @maxItems 256
     */
  segmentIds: string[];
  sourceExcerptId: string;
  sourceExcerptRevision: string;
  sourceStreamId: string;
  transcriptArtifactId: string;
}
