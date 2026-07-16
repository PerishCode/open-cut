import type { RoughCutLaneBindingInput } from './roughCutLaneBindingInput';

export interface RoughCutDerivationItemInput {
  audio?: RoughCutLaneBindingInput;
  sourceExcerptId: string;
  video?: RoughCutLaneBindingInput;
}
