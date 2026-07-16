
export type SourcePositionResultOperation = typeof SourcePositionResultOperation[keyof typeof SourcePositionResultOperation];


export const SourcePositionResultOperation = {
  settle: 'settle',
  previous: 'previous',
  next: 'next',
} as const;
