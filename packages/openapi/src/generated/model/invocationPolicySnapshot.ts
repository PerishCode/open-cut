import type { InvocationPolicy } from './invocationPolicy';

export interface InvocationPolicySnapshot {
  effective: InvocationPolicy;
  persisted: InvocationPolicy;
  /** @minimum 1 */
  settingsRevision: string;
}
