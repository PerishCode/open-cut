
export type NarrativeNodeStateEvidenceStatus = typeof NarrativeNodeStateEvidenceStatus[keyof typeof NarrativeNodeStateEvidenceStatus];


export const NarrativeNodeStateEvidenceStatus = {
  exact: 'exact',
  stale: 'stale',
} as const;
