import type { SourceGrantSummary } from './sourceGrantSummary';

export interface SourceGrantResult {
  grant: SourceGrantSummary;
  replayed: boolean;
}
