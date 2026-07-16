import type { VisualIntentStatePurpose } from './visualIntentStatePurpose';

export interface VisualIntentState {
  afterNodeId?: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  description: string;
  documentId: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  parentId: string;
  purpose: VisualIntentStatePurpose;
  revision: string;
  tombstoned: boolean;
}
