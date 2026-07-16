
export type EditOperationInputScope = typeof EditOperationInputScope[keyof typeof EditOperationInputScope];


export const EditOperationInputScope = {
  linked: 'linked',
  single: 'single',
} as const;
