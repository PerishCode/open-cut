import type { ArtifactSummary } from './artifactSummary';
import type { AssetViewAvailability } from './assetViewAvailability';
import type { AssetViewImportMode } from './assetViewImportMode';
import type { MediaFacts } from './mediaFacts';
import type { MediaJobSummary } from './mediaJobSummary';

export interface AssetView {
  acceptedFingerprint?: string;
  /** @maxItems 32 */
  artifacts: ArtifactSummary[];
  availability: AssetViewAvailability;
  displayName: string;
  facts?: MediaFacts;
  fingerprint?: string;
  id: string;
  importMode: AssetViewImportMode;
  /** @maxItems 32 */
  jobs: MediaJobSummary[];
  projectId: string;
  revision: string;
  tombstoned: boolean;
}
