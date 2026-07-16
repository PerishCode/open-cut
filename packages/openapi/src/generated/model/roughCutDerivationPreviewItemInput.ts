import type { RoughCutDerivationPreviewLaneInput } from './roughCutDerivationPreviewLaneInput';

export interface RoughCutDerivationPreviewItemInput {
  audio?: RoughCutDerivationPreviewLaneInput;
  sourceExcerptId: string;
  /** @pattern ^[1-9][0-9]*$ */
  sourceExcerptRevision: string;
  video?: RoughCutDerivationPreviewLaneInput;
}
