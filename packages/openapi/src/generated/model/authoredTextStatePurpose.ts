
export type AuthoredTextStatePurpose = typeof AuthoredTextStatePurpose[keyof typeof AuthoredTextStatePurpose];


export const AuthoredTextStatePurpose = {
  spoken: 'spoken',
  'on-screen': 'on-screen',
} as const;
