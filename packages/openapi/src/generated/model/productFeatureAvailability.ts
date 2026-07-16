import type { ProductFeatureAvailabilityFeature } from './productFeatureAvailabilityFeature';
import type { ProductFeatureAvailabilityReason } from './productFeatureAvailabilityReason';
import type { ProductFeatureAvailabilityState } from './productFeatureAvailabilityState';

export interface ProductFeatureAvailability {
  feature: ProductFeatureAvailabilityFeature;
  reason?: ProductFeatureAvailabilityReason;
  state: ProductFeatureAvailabilityState;
}
