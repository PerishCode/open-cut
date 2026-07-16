
export type ListProjectsStatus = typeof ListProjectsStatus[keyof typeof ListProjectsStatus];


export const ListProjectsStatus = {
  active: 'active',
  archived: 'archived',
  tombstoned: 'tombstoned',
} as const;
