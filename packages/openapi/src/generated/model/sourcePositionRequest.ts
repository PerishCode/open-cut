import type { RationalTime } from './rationalTime';
import type { SourcePositionRequestOperation } from './sourcePositionRequestOperation';

export interface SourcePositionRequest {
  operation: SourcePositionRequestOperation;
  resourceId: string;
  target: RationalTime;
}
