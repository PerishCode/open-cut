
export type ListEditTransactionsParams = {
/**
 * @pattern ^(0|[1-9][0-9]*)$
 */
after?: string;
/**
 * @minimum 1
 * @maximum 100
 */
limit?: number;
};
