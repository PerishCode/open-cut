import type { AlignmentTargetInput } from './alignmentTargetInput';
import type { CaptionDerivationPolicy } from './captionDerivationPolicy';
import type { ClipSplitOutputInput } from './clipSplitOutputInput';
import type { DerivedCaptionOutputInput } from './derivedCaptionOutputInput';
import type { DerivedRoughCutOutputInput } from './derivedRoughCutOutputInput';
import type { EditOperationInputAuthoredTextPurpose } from './editOperationInputAuthoredTextPurpose';
import type { EditOperationInputScope } from './editOperationInputScope';
import type { EditOperationInputType } from './editOperationInputType';
import type { EditOperationInputVisualIntentPurpose } from './editOperationInputVisualIntentPurpose';
import type { EditReference } from './editReference';
import type { RationalTime } from './rationalTime';
import type { RoughCutDerivationItemInput } from './roughCutDerivationItemInput';
import type { RoughCutDerivationPolicy } from './roughCutDerivationPolicy';
import type { TimeRange } from './timeRange';
import type { TranscriptCorrectionReferenceInput } from './transcriptCorrectionReferenceInput';

export interface EditOperationInput {
  acceptedFingerprint?: string;
  after?: EditReference;
  alignmentId?: string;
  /**
     * @minItems 1
     * @maxItems 64
     * @nullable
     */
  alignmentTargets?: AlignmentTargetInput[] | null;
  assetId?: string;
  authoredTextPurpose?: EditOperationInputAuthoredTextPurpose;
  captionId?: string;
  captionPolicy?: CaptionDerivationPolicy;
  clip?: EditReference;
  /**
     * @minItems 2
     * @maxItems 64
     * @nullable
     */
  clips?: EditReference[] | null;
  /**
     * @maxItems 256
     * @nullable
     */
  correctionRevisions?: TranscriptCorrectionReferenceInput[] | null;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  createAs?: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  createLinkGroupAs?: string;
  /**
     * @minItems 1
     * @maxItems 128
     * @nullable
     */
  derivedCaptions?: DerivedCaptionOutputInput[] | null;
  /**
     * @minItems 1
     * @maxItems 128
     * @nullable
     */
  derivedRoughCut?: DerivedRoughCutOutputInput[] | null;
  /** @maxLength 262144 */
  description?: string;
  enabled?: boolean;
  /** @maxLength 64 */
  language?: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  leftLinkGroupAs?: string;
  linkGroup?: EditReference;
  narrativeNode?: EditReference;
  nodeId?: string;
  parentId?: string;
  range?: TimeRange;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  rightLinkGroupAs?: string;
  /**
     * @minItems 1
     * @maxItems 128
     * @nullable
     */
  roughCutItems?: RoughCutDerivationItemInput[] | null;
  /** @pattern ^[a-z][a-z0-9_-]{0,39}$ */
  roughCutLocalPrefix?: string;
  roughCutOutputDigest?: string;
  roughCutPolicy?: RoughCutDerivationPolicy;
  roughCutTimelineStart?: RationalTime;
  scope?: EditOperationInputScope;
  sourceRange?: TimeRange;
  sourceStreamId?: string;
  splitAt?: RationalTime;
  /**
     * @minItems 1
     * @maxItems 64
     * @nullable
     */
  splitOutputs?: ClipSplitOutputInput[] | null;
  /** @maxLength 262144 */
  text?: string;
  timelineRange?: TimeRange;
  timelineStart?: RationalTime;
  /** @maxLength 262144 */
  title?: string;
  trackId?: string;
  transcriptArtifactId?: string;
  transcriptCorrectionId?: string;
  /**
     * @maxItems 256
     * @nullable
     */
  transcriptSegmentIds?: string[] | null;
  type: EditOperationInputType;
  visualIntentPurpose?: EditOperationInputVisualIntentPurpose;
}
