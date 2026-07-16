
export type CLIChallengeRequestMethod = typeof CLIChallengeRequestMethod[keyof typeof CLIChallengeRequestMethod];


export const CLIChallengeRequestMethod = {
  GET: 'GET',
  POST: 'POST',
} as const;
