
export type SourcePositionRequestOperation = typeof SourcePositionRequestOperation[keyof typeof SourcePositionRequestOperation];


export const SourcePositionRequestOperation = {
  settle: 'settle',
  previous: 'previous',
  next: 'next',
} as const;
