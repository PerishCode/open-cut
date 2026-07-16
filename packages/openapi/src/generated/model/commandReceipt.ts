import type { CommandReceiptClass } from './commandReceiptClass';
import type { CommandReceiptRef } from './commandReceiptRef';
import type { CommandReceiptSchema } from './commandReceiptSchema';
import type { CommandReceiptStatus } from './commandReceiptStatus';

export interface CommandReceipt {
  /** @pattern ^[1-9][0-9]*$ */
  activityCursor?: string;
  class: CommandReceiptClass;
  /**
     * @minLength 3
     * @maxLength 128
     */
  command: string;
  commandFingerprint: string;
  createdAt: string;
  id: string;
  inputDigest: string;
  /** @pattern ^[1-9][0-9]*$ */
  ordinal: string;
  projectId: string;
  /** @pattern ^[1-9][0-9]*$ */
  projectRevision?: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId?: string;
  resultDigest: string;
  /** @maxItems 256 */
  resultRefs: CommandReceiptRef[];
  runId: string;
  schema: CommandReceiptSchema;
  status: CommandReceiptStatus;
  turnId: string;
}
