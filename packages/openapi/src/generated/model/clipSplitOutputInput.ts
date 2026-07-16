import type { EditReference } from './editReference';

export interface ClipSplitOutputInput {
  clip: EditReference;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  leftAs: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  rightAs: string;
}
