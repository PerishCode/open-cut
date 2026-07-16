
export type ReadTranscriptParams = {
/**
 * @maxLength 36
 */
artifactId?: string;
/**
 * @maxLength 10
 */
after?: string;
/**
 * @minimum 1
 * @maximum 50
 */
limit?: number;
};
