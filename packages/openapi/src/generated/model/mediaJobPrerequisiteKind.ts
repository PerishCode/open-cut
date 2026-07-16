
export type MediaJobPrerequisiteKind = typeof MediaJobPrerequisiteKind[keyof typeof MediaJobPrerequisiteKind];


export const MediaJobPrerequisiteKind = {
  'fingerprint-required': 'fingerprint-required',
  'facts-required': 'facts-required',
  'model-required': 'model-required',
  'executor-required': 'executor-required',
} as const;
