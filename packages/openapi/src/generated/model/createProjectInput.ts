import type { SequenceFormat } from './sequenceFormat';

export interface CreateProjectInput {
  format?: SequenceFormat;
  /**
     * @minLength 1
     * @maxLength 200
     */
  name: string;
  /**
     * Creator gesture request identity
     * @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$
     */
  requestId: string;
}
