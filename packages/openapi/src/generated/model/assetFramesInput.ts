import type { RationalTime } from './rationalTime';

export interface AssetFramesInput {
  /** Asset to sample */
  assetId: string;
  /** Exact video SourceStream to sample */
  sourceStreamId: string;
  /**
     * Strictly increasing exact source times
     * @minItems 1
     * @maxItems 8
     * @nullable
     */
  times: RationalTime[] | null;
}
