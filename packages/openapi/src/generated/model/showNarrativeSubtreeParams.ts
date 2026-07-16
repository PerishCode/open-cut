
export type ShowNarrativeSubtreeParams = {
parentId: string;
/**
 * @maxLength 512
 */
after?: string;
/**
 * @minimum 1
 * @maximum 200
 */
limit?: number;
};
