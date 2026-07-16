
export type ProjectSummaryStatus = typeof ProjectSummaryStatus[keyof typeof ProjectSummaryStatus];


export const ProjectSummaryStatus = {
  active: 'active',
  archived: 'archived',
  tombstoned: 'tombstoned',
} as const;
