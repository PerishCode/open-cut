import type { RationalTime } from './rationalTime';
import type { SequenceFormatAudioLayout } from './sequenceFormatAudioLayout';
import type { SequenceFormatColorPolicy } from './sequenceFormatColorPolicy';

export interface SequenceFormat {
  audioLayout: SequenceFormatAudioLayout;
  /**
     * @minimum 8000
     * @maximum 384000
     */
  audioSampleRate: number;
  /**
     * @minimum 16
     * @maximum 16384
     */
  canvasHeight: number;
  /**
     * @minimum 16
     * @maximum 16384
     */
  canvasWidth: number;
  colorPolicy: SequenceFormatColorPolicy;
  frameRate: RationalTime;
  pixelAspect: RationalTime;
}
