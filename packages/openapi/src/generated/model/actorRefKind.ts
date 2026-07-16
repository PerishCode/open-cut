
export type ActorRefKind = typeof ActorRefKind[keyof typeof ActorRefKind];


export const ActorRefKind = {
  creator: 'creator',
  agent: 'agent',
} as const;
