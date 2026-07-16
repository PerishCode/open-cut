import type { EditImpactClass } from './editImpactClass';
import type { EditImpactClassifier } from './editImpactClassifier';

export interface EditImpact {
  class: EditImpactClass;
  classifier: EditImpactClassifier;
  requiresApproval: boolean;
}
