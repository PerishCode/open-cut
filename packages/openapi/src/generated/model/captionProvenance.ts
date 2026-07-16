import type { CaptionDerivationProvenance } from './captionDerivationProvenance';
import type { CaptionProvenanceKind } from './captionProvenanceKind';

export interface CaptionProvenance {
  derivation?: CaptionDerivationProvenance;
  kind: CaptionProvenanceKind;
}
