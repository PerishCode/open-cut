import type { EditReference } from './editReference';

export interface TranscriptCorrectionReferenceInput {
  correction: EditReference;
  /** @pattern ^[1-9][0-9]*$ */
  revision?: string;
}
