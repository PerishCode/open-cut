import type { ExportJobDataState } from './exportJobDataState';

export interface ExportJobData {
  createdAt: string;
  id: string;
  /**
     * @minimum 0
     * @maximum 10000
     */
  progressBasisPoints: number;
  retryOfJobId?: string;
  rootJobId: string;
  state: ExportJobDataState;
  /**
     * @minLength 1
     * @maxLength 64
     */
  terminalErrorCode?: string;
  updatedAt: string;
}
