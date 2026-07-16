
export type ListCreatorEditTransactionsParams = {
/**
 * @pattern ^(0|[1-9][0-9]*)$
 */
before?: string;
/**
 * @minimum 1
 * @maximum 50
 */
limit?: number;
};
