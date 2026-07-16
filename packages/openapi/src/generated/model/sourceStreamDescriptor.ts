import type { AudioStreamFacts } from './audioStreamFacts';
import type { RationalTime } from './rationalTime';
import type { SourceStreamDescriptorMediaType } from './sourceStreamDescriptorMediaType';
import type { VideoStreamFacts } from './videoStreamFacts';

export interface SourceStreamDescriptor {
  audio?: AudioStreamFacts;
  /**
     * @minLength 1
     * @maxLength 128
     */
  codec: string;
  /** @maxLength 128 */
  codecProfile?: string;
  /** @maxLength 64 */
  codecTag?: string;
  /** @maxItems 32 */
  dispositions: string[];
  duration?: RationalTime;
  /** @minimum 0 */
  index: number;
  /** @maxLength 64 */
  language?: string;
  mediaType: SourceStreamDescriptorMediaType;
  startTime?: RationalTime;
  timeBase: RationalTime;
  video?: VideoStreamFacts;
}
