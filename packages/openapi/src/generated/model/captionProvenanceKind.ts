
export type CaptionProvenanceKind = typeof CaptionProvenanceKind[keyof typeof CaptionProvenanceKind];


export const CaptionProvenanceKind = {
  manual: 'manual',
  'transcript-derivation': 'transcript-derivation',
} as const;
