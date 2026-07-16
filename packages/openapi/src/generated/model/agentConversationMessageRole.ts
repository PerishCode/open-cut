
export type AgentConversationMessageRole = typeof AgentConversationMessageRole[keyof typeof AgentConversationMessageRole];


export const AgentConversationMessageRole = {
  creator: 'creator',
  agent: 'agent',
  notice: 'notice',
} as const;
