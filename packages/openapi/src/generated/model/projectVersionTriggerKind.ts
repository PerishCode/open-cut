
export type ProjectVersionTriggerKind = typeof ProjectVersionTriggerKind[keyof typeof ProjectVersionTriggerKind];


export const ProjectVersionTriggerKind = {
  turn: 'turn',
  version: 'version',
} as const;
