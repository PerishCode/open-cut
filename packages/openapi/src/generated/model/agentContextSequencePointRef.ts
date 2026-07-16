import type { RationalTime } from './rationalTime';

export interface AgentContextSequencePointRef {
  /** @pattern ^[1-9][0-9]*$ */
  revision: string;
  sequenceId: string;
  time: RationalTime;
}
