import type { CaptionProvenanceStatusContent } from './captionProvenanceStatusContent';
import type { CaptionProvenanceStatusEvidence } from './captionProvenanceStatusEvidence';

export interface CaptionProvenanceStatus {
  content: CaptionProvenanceStatusContent;
  evidence: CaptionProvenanceStatusEvidence;
}
