
export type ActivityScopeKind = typeof ActivityScopeKind[keyof typeof ActivityScopeKind];


export const ActivityScopeKind = {
  project: 'project',
  installation: 'installation',
} as const;
