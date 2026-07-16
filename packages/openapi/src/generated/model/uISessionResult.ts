import type { UISessionResultSchema } from './uISessionResultSchema';

export interface UISessionResult {
  expiresAt: string;
  schema: UISessionResultSchema;
  session: string;
}
