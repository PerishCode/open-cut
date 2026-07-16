
export type ListActivityParams = {
/**
 * Optional Project activity scope; defaults to the current installation
 * @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
 */
projectId?: string;
/**
 * Return events strictly after this scope-local cursor
 * @pattern ^(0|[1-9][0-9]*)$
 */
after?: string;
/**
 * Maximum activity events to return
 * @minimum 1
 * @maximum 500
 */
limit?: number;
};
