
export interface AgentBridgeTransitionInput {
  /** @pattern ^[1-9][0-9]*$ */
  expectedGeneration: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
