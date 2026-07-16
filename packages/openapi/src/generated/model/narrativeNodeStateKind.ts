
export type NarrativeNodeStateKind = typeof NarrativeNodeStateKind[keyof typeof NarrativeNodeStateKind];


export const NarrativeNodeStateKind = {
  section: 'section',
  'authored-text': 'authored-text',
  'source-excerpt': 'source-excerpt',
  'visual-intent': 'visual-intent',
  note: 'note',
} as const;
