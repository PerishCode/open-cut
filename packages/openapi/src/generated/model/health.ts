import type { HealthService } from './healthService';

export interface Health {
  /** Whether the API is ready */
  ok: boolean;
  /** Service reporting health */
  service: HealthService;
}
