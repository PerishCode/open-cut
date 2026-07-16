
export interface NarrativeSectionState {
  afterNodeId?: string;
  documentId: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  parentId?: string;
  revision: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  title: string;
  tombstoned: boolean;
}
