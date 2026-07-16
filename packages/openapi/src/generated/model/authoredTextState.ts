import type { AuthoredTextStatePurpose } from './authoredTextStatePurpose';

export interface AuthoredTextState {
  afterNodeId?: string;
  documentId: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  parentId: string;
  purpose: AuthoredTextStatePurpose;
  revision: string;
  /** @maxLength 262144 */
  text: string;
  tombstoned: boolean;
}
