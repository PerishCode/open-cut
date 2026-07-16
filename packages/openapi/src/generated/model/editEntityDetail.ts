import type { AlignmentState } from './alignmentState';
import type { AuthoredTextState } from './authoredTextState';
import type { CaptionState } from './captionState';
import type { ClipState } from './clipState';
import type { EditEntityDetailKind } from './editEntityDetailKind';
import type { EditEntityDetailSourceExcerptEvidenceStatus } from './editEntityDetailSourceExcerptEvidenceStatus';
import type { LinkGroupState } from './linkGroupState';
import type { NarrativeSectionState } from './narrativeSectionState';
import type { NoteState } from './noteState';
import type { SourceExcerptState } from './sourceExcerptState';
import type { TranscriptCorrectionState } from './transcriptCorrectionState';
import type { VisualIntentState } from './visualIntentState';

export interface EditEntityDetail {
  activityCursor: string;
  alignment?: AlignmentState;
  authoredText?: AuthoredTextState;
  caption?: CaptionState;
  clip?: ClipState;
  kind: EditEntityDetailKind;
  linkGroup?: LinkGroupState;
  note?: NoteState;
  section?: NarrativeSectionState;
  sourceExcerpt?: SourceExcerptState;
  sourceExcerptEvidenceStatus?: EditEntityDetailSourceExcerptEvidenceStatus;
  transcriptCorrection?: TranscriptCorrectionState;
  visualIntent?: VisualIntentState;
}
