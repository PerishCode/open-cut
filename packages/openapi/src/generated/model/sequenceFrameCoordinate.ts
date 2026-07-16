import type { RationalTime } from './rationalTime';

export interface SequenceFrameCoordinate {
  frameIndex: string;
  requestedTime: RationalTime;
  sequenceTime: RationalTime;
}
