import type { TranscriptArtifactView } from './transcriptArtifactView';
import type { TranscriptCorrectionView } from './transcriptCorrectionView';
import type { TranscriptReadPageSchema } from './transcriptReadPageSchema';
import type { TranscriptSegmentView } from './transcriptSegmentView';

export interface TranscriptReadPage {
  activityCursor: string;
  artifact: TranscriptArtifactView;
  /** @maxItems 256 */
  corrections: TranscriptCorrectionView[];
  /** @maxLength 10 */
  nextAfter?: string;
  schema: TranscriptReadPageSchema;
  /** @maxItems 50 */
  segments: TranscriptSegmentView[];
}
