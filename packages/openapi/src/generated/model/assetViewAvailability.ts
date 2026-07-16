
export type AssetViewAvailability = typeof AssetViewAvailability[keyof typeof AssetViewAvailability];


export const AssetViewAvailability = {
  identifying: 'identifying',
  online: 'online',
  changed: 'changed',
  missing: 'missing',
  managed: 'managed',
  unreadable: 'unreadable',
} as const;
