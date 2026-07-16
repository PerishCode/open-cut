
export type AssetStateImportMode = typeof AssetStateImportMode[keyof typeof AssetStateImportMode];


export const AssetStateImportMode = {
  referenced: 'referenced',
  managed: 'managed',
} as const;
