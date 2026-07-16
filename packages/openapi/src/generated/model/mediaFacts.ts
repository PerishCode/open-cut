import type { RationalTime } from './rationalTime';
import type { SourceStream } from './sourceStream';

export interface MediaFacts {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  bitRate?: string;
  /**
     * @minLength 1
     * @maxLength 128
     */
  container: string;
  /** @maxItems 32 */
  containerAliases: string[];
  duration?: RationalTime;
  startTime?: RationalTime;
  /** @maxItems 64 */
  streams: SourceStream[];
}
