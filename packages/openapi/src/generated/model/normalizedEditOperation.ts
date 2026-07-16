import type { AlignmentState } from './alignmentState';
import type { AssetState } from './assetState';
import type { CaptionState } from './captionState';
import type { ClipState } from './clipState';
import type { LinkGroupState } from './linkGroupState';
import type { NarrativeNodeState } from './narrativeNodeState';
import type { NormalizedEditOperationType } from './normalizedEditOperationType';
import type { TranscriptCorrectionState } from './transcriptCorrectionState';

export interface NormalizedEditOperation {
  alignment?: AlignmentState;
  asset?: AssetState;
  caption?: CaptionState;
  clip?: ClipState;
  linkGroup?: LinkGroupState;
  narrativeNode?: NarrativeNodeState;
  transcriptCorrection?: TranscriptCorrectionState;
  type: NormalizedEditOperationType;
}
