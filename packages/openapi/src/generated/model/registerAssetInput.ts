import type { RegisterAssetInputImportMode } from './registerAssetInputImportMode';

export interface RegisterAssetInput {
  /** @pattern ^[1-9][0-9]*$ */
  expectedProjectRevision: string;
  importMode: RegisterAssetInputImportMode;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
  sourceGrantId: string;
}
