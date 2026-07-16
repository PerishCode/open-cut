
export type ActivityActorKind = typeof ActivityActorKind[keyof typeof ActivityActorKind];


export const ActivityActorKind = {
  creator: 'creator',
  agent: 'agent',
} as const;
