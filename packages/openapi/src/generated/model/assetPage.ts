import type { AssetView } from './assetView';

export interface AssetPage {
  activityCursor: string;
  /** @maxItems 100 */
  assets: AssetView[];
  nextAfter?: string;
}
