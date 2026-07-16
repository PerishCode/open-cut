import type { ArtifactSummaryKind } from './artifactSummaryKind';
import type { ArtifactSummaryState } from './artifactSummaryState';

export interface ArtifactSummary {
  byteSize: string;
  contentDigest: string;
  createdAt: string;
  id: string;
  inputFingerprint: string;
  kind: ArtifactSummaryKind;
  producerVersion: string;
  state: ArtifactSummaryState;
}
