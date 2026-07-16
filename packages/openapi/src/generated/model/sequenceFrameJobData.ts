import type { SequenceFrameJobDataState } from './sequenceFrameJobDataState';

export interface SequenceFrameJobData {
  createdAt: string;
  id: string;
  /**
     * @minimum 0
     * @maximum 10000
     */
  progressBasisPoints: number;
  state: SequenceFrameJobDataState;
  /**
     * @minLength 1
     * @maxLength 256
     */
  terminalErrorCode?: string;
  updatedAt: string;
}
