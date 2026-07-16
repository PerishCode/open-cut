import type { InvocationPolicyOverrideOutput } from './invocationPolicyOverrideOutput';

export interface InvocationPolicyOverride {
  output?: InvocationPolicyOverrideOutput;
  /**
     * @minimum 250
     * @maximum 30000
     */
  waitMilliseconds?: number;
}
