import type { AuthoredTextState } from './authoredTextState';
import type { NarrativeNodeStateEvidenceStatus } from './narrativeNodeStateEvidenceStatus';
import type { NarrativeNodeStateKind } from './narrativeNodeStateKind';
import type { NarrativeSectionState } from './narrativeSectionState';
import type { NoteState } from './noteState';
import type { SourceExcerptState } from './sourceExcerptState';
import type { VisualIntentState } from './visualIntentState';

export interface NarrativeNodeState {
  authoredText?: AuthoredTextState;
  evidenceStatus?: NarrativeNodeStateEvidenceStatus;
  kind: NarrativeNodeStateKind;
  note?: NoteState;
  section?: NarrativeSectionState;
  sourceExcerpt?: SourceExcerptState;
  visualIntent?: VisualIntentState;
}
