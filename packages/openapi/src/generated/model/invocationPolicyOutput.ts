
export type InvocationPolicyOutput = typeof InvocationPolicyOutput[keyof typeof InvocationPolicyOutput];


export const InvocationPolicyOutput = {
  json: 'json',
  human: 'human',
} as const;
