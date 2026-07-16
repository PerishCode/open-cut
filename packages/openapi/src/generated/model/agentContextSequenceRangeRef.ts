import type { TimeRange } from './timeRange';

export interface AgentContextSequenceRangeRef {
  range: TimeRange;
  /** @pattern ^[1-9][0-9]*$ */
  revision: string;
  sequenceId: string;
}
