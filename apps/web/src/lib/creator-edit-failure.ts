import { CreatorEditError } from "@open-cut/contracts";

/** The product-authored failure sentence carried by a Contracts edit error, if any. */
export function editFailureReason(error: Error): string | undefined {
  return error instanceof CreatorEditError ? error.reason : undefined;
}
