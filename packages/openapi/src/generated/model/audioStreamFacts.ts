
export interface AudioStreamFacts {
  /** @maxLength 128 */
  channelLayout?: string;
  /**
     * @minimum 1
     * @maximum 64
     */
  channels: number;
  /** @maxLength 64 */
  sampleFormat?: string;
  /**
     * @minimum 1
     * @maximum 768000
     */
  sampleRate: number;
}
