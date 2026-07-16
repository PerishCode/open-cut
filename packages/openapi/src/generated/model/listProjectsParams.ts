import type { ListProjectsStatus } from './listProjectsStatus';

export type ListProjectsParams = {
/**
 * Optional lifecycle filter
 */
status?: ListProjectsStatus;
/**
 * Opaque query-local continuation cursor
 * @maxLength 512
 */
after?: string;
/**
 * Maximum summaries to return
 * @minimum 1
 * @maximum 100
 */
limit?: number;
};
