import type { ExportArtifactDataAudioCodec } from './exportArtifactDataAudioCodec';
import type { ExportArtifactDataAudioSampleRate } from './exportArtifactDataAudioSampleRate';
import type { ExportArtifactDataChannelLayout } from './exportArtifactDataChannelLayout';
import type { ExportArtifactDataPixelFormat } from './exportArtifactDataPixelFormat';
import type { ExportArtifactDataVerification } from './exportArtifactDataVerification';
import type { ExportArtifactDataVideoCodec } from './exportArtifactDataVideoCodec';
import type { RationalTime } from './rationalTime';

export interface ExportArtifactData {
  audioCodec: ExportArtifactDataAudioCodec;
  audioSampleCount: string;
  /** @minimum 0 */
  audioSampleRate: ExportArtifactDataAudioSampleRate;
  byteSize: string;
  /** @minimum 2 */
  canvasHeight: number;
  /** @minimum 2 */
  canvasWidth: number;
  channelLayout: ExportArtifactDataChannelLayout;
  contentDigest: string;
  frameRate: RationalTime;
  id: string;
  pixelFormat: ExportArtifactDataPixelFormat;
  presentationDuration: RationalTime;
  semanticDuration: RationalTime;
  verification: ExportArtifactDataVerification;
  videoCodec: ExportArtifactDataVideoCodec;
  videoFrameCount: string;
}
