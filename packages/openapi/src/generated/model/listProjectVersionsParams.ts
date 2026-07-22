
export type ListProjectVersionsParams = {
/**
 * @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
 */
before?: string;
/**
 * @minimum 1
 * @maximum 50
 */
limit?: number;
};
