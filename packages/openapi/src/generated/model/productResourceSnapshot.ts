import type { ProductResourceSnapshotSchema } from './productResourceSnapshotSchema';
import type { ProductResourceView } from './productResourceView';

export interface ProductResourceSnapshot {
  /** @maxItems 128 */
  resources: ProductResourceView[];
  schema: ProductResourceSnapshotSchema;
}
