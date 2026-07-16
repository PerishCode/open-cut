
export type AssetViewImportMode = typeof AssetViewImportMode[keyof typeof AssetViewImportMode];


export const AssetViewImportMode = {
  referenced: 'referenced',
  managed: 'managed',
} as const;
