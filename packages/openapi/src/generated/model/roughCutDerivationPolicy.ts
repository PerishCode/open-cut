import type { RationalTime } from './rationalTime';
import type { RoughCutDerivationPolicyAvGrouping } from './roughCutDerivationPolicyAvGrouping';
import type { RoughCutDerivationPolicyId } from './roughCutDerivationPolicyId';
import type { RoughCutDerivationPolicyOrdering } from './roughCutDerivationPolicyOrdering';
import type { RoughCutDerivationPolicyOverwrite } from './roughCutDerivationPolicyOverwrite';
import type { RoughCutDerivationPolicyRate } from './roughCutDerivationPolicyRate';
import type { RoughCutDerivationPolicySourceHandles } from './roughCutDerivationPolicySourceHandles';

export interface RoughCutDerivationPolicy {
  avGrouping: RoughCutDerivationPolicyAvGrouping;
  id: RoughCutDerivationPolicyId;
  interExcerptGap: RationalTime;
  ordering: RoughCutDerivationPolicyOrdering;
  overwrite: RoughCutDerivationPolicyOverwrite;
  rate: RoughCutDerivationPolicyRate;
  sourceHandles: RoughCutDerivationPolicySourceHandles;
}
