
export type AssetDetailAvailability = typeof AssetDetailAvailability[keyof typeof AssetDetailAvailability];


export const AssetDetailAvailability = {
  identifying: 'identifying',
  online: 'online',
  changed: 'changed',
  missing: 'missing',
  managed: 'managed',
  unreadable: 'unreadable',
} as const;
