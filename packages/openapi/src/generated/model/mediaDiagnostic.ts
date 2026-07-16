import type { MediaDiagnosticCode } from './mediaDiagnosticCode';
import type { MediaDiagnosticRecovery } from './mediaDiagnosticRecovery';
import type { MediaDiagnosticSeverity } from './mediaDiagnosticSeverity';
import type { MediaDiagnosticSubjectKind } from './mediaDiagnosticSubjectKind';

export interface MediaDiagnostic {
  code: MediaDiagnosticCode;
  recovery: MediaDiagnosticRecovery;
  severity: MediaDiagnosticSeverity;
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  subjectId: string;
  subjectKind: MediaDiagnosticSubjectKind;
}
