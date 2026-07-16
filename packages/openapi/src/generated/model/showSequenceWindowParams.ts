
export type ShowSequenceWindowParams = {
trackId?: string;
/**
 * @pattern ^(0|-[1-9][0-9]*|[1-9][0-9]*)$
 */
startValue: string;
/**
 * @minimum 1
 */
startScale: number;
/**
 * @pattern ^[1-9][0-9]*$
 */
durationValue: string;
/**
 * @minimum 1
 */
durationScale: number;
/**
 * @maxLength 512
 */
after?: string;
/**
 * @minimum 1
 * @maximum 512
 */
limit?: number;
};
