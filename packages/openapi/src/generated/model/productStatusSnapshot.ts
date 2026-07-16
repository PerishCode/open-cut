import type { ProductFeatureAvailability } from './productFeatureAvailability';
import type { ProductStatusSnapshotSchema } from './productStatusSnapshotSchema';

export interface ProductStatusSnapshot {
  /**
     * @minItems 5
     * @maxItems 5
     */
  features: ProductFeatureAvailability[];
  schema: ProductStatusSnapshotSchema;
}
