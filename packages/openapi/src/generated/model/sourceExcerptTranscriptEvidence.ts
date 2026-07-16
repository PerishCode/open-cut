import type { TranscriptCorrectionRevisionRef } from './transcriptCorrectionRevisionRef';

export interface SourceExcerptTranscriptEvidence {
  artifactId: string;
  /** @maxItems 256 */
  correctionRevisions: TranscriptCorrectionRevisionRef[];
  /**
     * @minItems 1
     * @maxItems 256
     */
  segmentIds: string[];
  sourceStreamId: string;
}
