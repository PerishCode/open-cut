import type { RationalTime } from './rationalTime';

export interface SequencePreviewMediaFacts {
  audioCodec: string;
  audioSampleCount: string;
  /** @minimum 0 */
  audioSampleRate: number;
  /** @minimum 0 */
  canvasHeight: number;
  /** @minimum 0 */
  canvasWidth: number;
  channelLayout: string;
  frameRate: RationalTime;
  pixelFormat: string;
  presentationDuration: RationalTime;
  semanticDuration: RationalTime;
  videoCodec: string;
  videoFrameCount: string;
}
