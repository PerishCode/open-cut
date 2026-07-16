import type { ProjectSummaryStatus } from './projectSummaryStatus';

export interface ProjectSummary {
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  id: string;
  /** @pattern ^(0|[1-9][0-9]*)$ */
  lifecycleRevision: string;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  mainSequenceId: string;
  name: string;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  narrativeDocumentId: string;
  /** @pattern ^(0|[1-9][0-9]*)$ */
  revision: string;
  status: ProjectSummaryStatus;
}
