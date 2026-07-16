import type { ProductResourceViewKind } from './productResourceViewKind';
import type { ProductResourceViewState } from './productResourceViewState';

export interface ProductResourceView {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  byteSize: string;
  /** @maxLength 64 */
  failureCode?: string;
  jobId?: string;
  kind: ProductResourceViewKind;
  /** @pattern ^[a-z][a-z0-9.-]{0,127}$ */
  name: string;
  /**
     * @minLength 1
     * @maxLength 128
     */
  profile: string;
  /**
     * @minimum 0
     * @maximum 10000
     */
  progressBasisPoints: number;
  resourceId?: string;
  state: ProductResourceViewState;
  updatedAt?: string;
  /**
     * @minLength 1
     * @maxLength 128
     */
  version: string;
}
