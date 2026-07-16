
export type ProductFeatureAvailabilityState = typeof ProductFeatureAvailabilityState[keyof typeof ProductFeatureAvailabilityState];


export const ProductFeatureAvailabilityState = {
  available: 'available',
  unavailable: 'unavailable',
} as const;
