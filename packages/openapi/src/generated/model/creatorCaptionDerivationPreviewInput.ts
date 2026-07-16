
export interface CreatorCaptionDerivationPreviewInput {
  clipId: string;
  /** @pattern ^[1-9][0-9]*$ */
  clipRevision: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,39}$ */
  localPrefix: string;
  sourceExcerptId: string;
  /** @pattern ^[1-9][0-9]*$ */
  sourceExcerptRevision: string;
  trackId: string;
  /** @pattern ^[1-9][0-9]*$ */
  trackRevision: string;
}
