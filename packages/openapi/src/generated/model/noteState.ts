
export interface NoteState {
  afterNodeId?: string;
  documentId: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  parentId: string;
  revision: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  text: string;
  tombstoned: boolean;
}
