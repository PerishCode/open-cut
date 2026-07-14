
/**
 * Service reporting health
 */
export type HealthService = typeof HealthService[keyof typeof HealthService];


export const HealthService = {
  api: 'api',
} as const;
