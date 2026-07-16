import type { RationalTime } from './rationalTime';

export interface SequenceFramesInput {
  /** Frame job lineage to continue */
  jobId?: string;
  /** Recoverable terminal frame job lineage to retry */
  retryJobId?: string;
  /** Exact committed Sequence revision to prepare */
  sequenceRevision?: string;
  /**
     * Strictly increasing exact Sequence times
     * @minItems 1
     * @maxItems 8
     * @nullable
     */
  times?: RationalTime[] | null;
}
