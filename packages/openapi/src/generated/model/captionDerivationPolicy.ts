import type { CaptionDerivationPolicyBoundaryPolicy } from './captionDerivationPolicyBoundaryPolicy';
import type { CaptionDerivationPolicyId } from './captionDerivationPolicyId';
import type { CaptionDerivationPolicyTimingPolicy } from './captionDerivationPolicyTimingPolicy';
import type { CaptionDerivationPolicyUnicodeSegmentationId } from './captionDerivationPolicyUnicodeSegmentationId';
import type { RationalTime } from './rationalTime';

export interface CaptionDerivationPolicy {
  boundaryPolicy: CaptionDerivationPolicyBoundaryPolicy;
  id: CaptionDerivationPolicyId;
  maximumDuration: RationalTime;
  maximumGap: RationalTime;
  /**
     * @minimum 42
     * @maximum 42
     */
  maximumLineGraphemes: number;
  /**
     * @minimum 2
     * @maximum 2
     */
  maximumLines: number;
  /**
     * @minimum 20
     * @maximum 20
     */
  maximumReadingRate: number;
  minimumDuration: RationalTime;
  timingPolicy: CaptionDerivationPolicyTimingPolicy;
  unicodeSegmentationId: CaptionDerivationPolicyUnicodeSegmentationId;
}
