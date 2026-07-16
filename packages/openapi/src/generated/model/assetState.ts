import type { AssetStateImportMode } from './assetStateImportMode';

export interface AssetState {
  acceptedFingerprint?: string;
  /**
     * @minLength 1
     * @maxLength 512
     */
  displayName: string;
  id: string;
  importMode: AssetStateImportMode;
  projectId: string;
  revision: string;
  sourceGrantId: string;
  tombstoned: boolean;
}
