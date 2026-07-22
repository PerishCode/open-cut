
export type ProjectVersionRetention = typeof ProjectVersionRetention[keyof typeof ProjectVersionRetention];


export const ProjectVersionRetention = {
  automatic: 'automatic',
  manual: 'manual',
  pinned: 'pinned',
} as const;
