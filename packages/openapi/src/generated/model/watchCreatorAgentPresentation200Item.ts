import type { AgentPresentationEnvelope } from './agentPresentationEnvelope';

export type WatchCreatorAgentPresentation200Item = {
  data: AgentPresentationEnvelope;
  /** The event name. */
  event: 'presentation';
  /** The event ID. */
  id?: number;
  /** The retry time in milliseconds. */
  retry?: number;
};
