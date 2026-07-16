import type { SequenceExportDeliveryLeaseMimeType } from './sequenceExportDeliveryLeaseMimeType';
import type { SequenceExportDeliveryLeaseSchema } from './sequenceExportDeliveryLeaseSchema';

export interface SequenceExportDeliveryLease {
  artifactId: string;
  byteLength: string;
  contentSha256: string;
  contentUrl: string;
  expiresAt: string;
  mimeType: SequenceExportDeliveryLeaseMimeType;
  schema: SequenceExportDeliveryLeaseSchema;
}
