
export type VisualIntentStatePurpose = typeof VisualIntentStatePurpose[keyof typeof VisualIntentStatePurpose];


export const VisualIntentStatePurpose = {
  'b-roll': 'b-roll',
  composition: 'composition',
  replacement: 'replacement',
} as const;
