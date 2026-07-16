import type { RationalTime } from './rationalTime';
import type { TranscriptArtifactViewRecognitionProfile } from './transcriptArtifactViewRecognitionProfile';

export interface TranscriptArtifactView {
  assetId: string;
  createdAt: string;
  /** @maxLength 64 */
  detectedLanguage: string;
  /**
     * @minLength 1
     * @maxLength 128
     */
  engineTarget: string;
  /**
     * @minLength 1
     * @maxLength 1024
     */
  engineVersion: string;
  id: string;
  isDefault: boolean;
  /**
     * @minimum 0
     * @maximum 10000
     */
  languageConfidenceBasisPoints?: number;
  /** @pattern ^[a-z][a-z0-9.-]{0,127}$ */
  modelName: string;
  /**
     * @minLength 1
     * @maxLength 128
     */
  modelVersion: string;
  normalizedSampleCount: string;
  recognitionProfile: TranscriptArtifactViewRecognitionProfile;
  sourceStartTime: RationalTime;
  sourceStreamId: string;
}
