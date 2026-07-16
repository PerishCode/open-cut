import type { MediaJobPrerequisiteKind } from './mediaJobPrerequisiteKind';

export interface MediaJobPrerequisite {
  /** @maxLength 256 */
  capability?: string;
  jobId?: string;
  kind: MediaJobPrerequisiteKind;
  /** @maxLength 256 */
  resourceId?: string;
}
