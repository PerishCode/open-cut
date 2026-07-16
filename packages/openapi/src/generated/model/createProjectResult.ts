import type { ProjectOverview } from './projectOverview';

export interface CreateProjectResult {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  installationActivityCursor: string;
  project: ProjectOverview;
  /** @pattern ^(0|[1-9][0-9]*)$ */
  projectActivityCursor: string;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  proposalId: string;
  replayed: boolean;
  /** @pattern ^sha256:[0-9a-f]{64}$ */
  requestDigest: string;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  transactionId: string;
}
