import type { ArtifactSummary } from './artifactSummary';
import type { AssetDetailAvailability } from './assetDetailAvailability';
import type { AssetState } from './assetState';
import type { MediaFacts } from './mediaFacts';
import type { MediaJobSummary } from './mediaJobSummary';

export interface AssetDetail {
  /** @maxItems 32 */
  artifacts: ArtifactSummary[];
  asset: AssetState;
  availability: AssetDetailAvailability;
  facts?: MediaFacts;
  fingerprint?: string;
  /** @maxItems 32 */
  jobs: MediaJobSummary[];
}
