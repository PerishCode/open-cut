
export type DeriveCaptionOperationParams = {
sourceExcerptId: string;
clipId: string;
trackId: string;
/**
 * @pattern ^[a-z][a-z0-9_-]{0,39}$
 */
localPrefix?: string;
};
