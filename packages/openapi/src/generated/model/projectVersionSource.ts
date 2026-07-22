
export type ProjectVersionSource = typeof ProjectVersionSource[keyof typeof ProjectVersionSource];


export const ProjectVersionSource = {
  genesis: 'genesis',
  manual: 'manual',
  'agent-turn': 'agent-turn',
  'pre-restore': 'pre-restore',
} as const;
