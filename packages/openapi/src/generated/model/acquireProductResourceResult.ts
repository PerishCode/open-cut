import type { ProductResourceView } from './productResourceView';

export interface AcquireProductResourceResult {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  activityCursor: string;
  replayed: boolean;
  resource: ProductResourceView;
}
