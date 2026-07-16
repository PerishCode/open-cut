import type { RationalTime } from './rationalTime';
import type { SourcePositionResultBoundary } from './sourcePositionResultBoundary';
import type { SourcePositionResultOperation } from './sourcePositionResultOperation';

export interface SourcePositionResult {
  assetId: string;
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  atEnd: boolean;
  atStart: boolean;
  audioStreamId?: string;
  boundary: SourcePositionResultBoundary;
  fingerprint: string;
  operation: SourcePositionResultOperation;
  projectId: string;
  proxyTime: RationalTime;
  requestedTime: RationalTime;
  resourceId: string;
  sourceTime: RationalTime;
  videoStreamId?: string;
}
