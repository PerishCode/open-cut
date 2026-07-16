import type { SourceGrantSummaryKind } from './sourceGrantSummaryKind';
import type { SourceGrantSummaryPlatform } from './sourceGrantSummaryPlatform';
import type { SourceGrantSummaryState } from './sourceGrantSummaryState';
import type { SourceObservation } from './sourceObservation';

export interface SourceGrantSummary {
  createdAt: string;
  /**
     * @minLength 1
     * @maxLength 512
     */
  displayName: string;
  id: string;
  kind: SourceGrantSummaryKind;
  observation: SourceObservation;
  platform: SourceGrantSummaryPlatform;
  state: SourceGrantSummaryState;
}
