import type { ActivityScopeKind } from './activityScopeKind';

export interface ActivityScope {
  /**
     * @minLength 1
     * @maxLength 128
     */
  id: string;
  kind: ActivityScopeKind;
}
