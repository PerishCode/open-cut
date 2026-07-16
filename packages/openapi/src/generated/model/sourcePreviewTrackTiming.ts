import type { RationalTime } from './rationalTime';

export interface SourcePreviewTrackTiming {
  coverageDuration?: RationalTime;
  coverageStart: RationalTime;
  proxyStartTime: RationalTime;
  proxyTimeBase: RationalTime;
  sourceStartTime: RationalTime;
  sourceStreamId: string;
  sourceTimeBase: RationalTime;
}
