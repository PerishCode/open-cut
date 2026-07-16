import type { RationalTime } from './rationalTime';
import type { VideoStreamFactsRotation } from './videoStreamFactsRotation';

export interface VideoStreamFacts {
  averageRate?: RationalTime;
  /**
     * @minimum 0
     * @maximum 32768
     */
  codedHeight?: number;
  /**
     * @minimum 0
     * @maximum 32768
     */
  codedWidth?: number;
  /** @maxLength 64 */
  colorPrimaries?: string;
  /** @maxLength 64 */
  colorRange?: string;
  /** @maxLength 64 */
  colorSpace?: string;
  /** @maxLength 64 */
  colorTransfer?: string;
  /**
     * @minimum 1
     * @maximum 32768
     */
  height: number;
  nominalRate?: RationalTime;
  pixelAspect?: RationalTime;
  /** @maxLength 64 */
  pixelFormat?: string;
  rotation: VideoStreamFactsRotation;
  /**
     * @minimum 1
     * @maximum 32768
     */
  width: number;
}
