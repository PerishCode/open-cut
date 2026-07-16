import type { InvocationPolicyOutput } from './invocationPolicyOutput';

export interface InvocationPolicy {
  output: InvocationPolicyOutput;
  /**
     * @minimum 250
     * @maximum 30000
     */
  waitMilliseconds: number;
}
