import type { RationalTime } from './rationalTime';
import type { RoughCutDerivationPreviewItemInput } from './roughCutDerivationPreviewItemInput';

export interface RoughCutDerivationPreviewInput {
  /**
     * @minItems 1
     * @maxItems 128
     */
  items: RoughCutDerivationPreviewItemInput[];
  /** @pattern ^[a-z][a-z0-9_-]{0,39}$ */
  localPrefix: string;
  timelineStart: RationalTime;
}
