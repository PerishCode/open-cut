
export type ProductFeatureAvailabilityReason = typeof ProductFeatureAvailabilityReason[keyof typeof ProductFeatureAvailabilityReason];


export const ProductFeatureAvailabilityReason = {
  'not-installed': 'not-installed',
  'not-qualified': 'not-qualified',
  'invalid-closure': 'invalid-closure',
} as const;
