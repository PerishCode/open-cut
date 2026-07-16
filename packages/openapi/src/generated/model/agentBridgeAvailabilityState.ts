
export type AgentBridgeAvailabilityState = typeof AgentBridgeAvailabilityState[keyof typeof AgentBridgeAvailabilityState];


export const AgentBridgeAvailabilityState = {
  available: 'available',
  missing: 'missing',
  unauthenticated: 'unauthenticated',
  incompatible: 'incompatible',
} as const;
