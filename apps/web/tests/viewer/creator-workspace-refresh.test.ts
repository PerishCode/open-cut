import { describe, expect, it, vi } from "vitest";

import { createBackgroundWorkspaceInvalidation } from "../../src/lib/creator-workspace-refresh.js";

describe("createBackgroundWorkspaceInvalidation", () => {
  it("reconciles project activity through the preserve-ready load path", async () => {
    const load = vi.fn(async (_signal: AbortSignal | undefined, preserveReady: boolean) => {
      expect(preserveReady).toBe(true);
      return { status: "ready" as const };
    });

    const onActivity = createBackgroundWorkspaceInvalidation(load);
    await onActivity();

    expect(load).toHaveBeenCalledTimes(1);
    expect(load).toHaveBeenCalledWith(undefined, true);
  });

  it("swallows transient background refresh failure without blanking ready state", async () => {
    const load = vi.fn(async (_signal: AbortSignal | undefined, preserveReady: boolean) => {
      expect(preserveReady).toBe(true);
      throw new Error("transient background refresh failed");
    });

    const onActivity = createBackgroundWorkspaceInvalidation(load);
    await expect(onActivity()).resolves.toBeUndefined();
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("never invokes load with preserveReady=false from background invalidation", async () => {
    const preserveFlags: boolean[] = [];
    const load = vi.fn(async (_signal: AbortSignal | undefined, preserveReady: boolean) => {
      preserveFlags.push(preserveReady);
    });

    const onActivity = createBackgroundWorkspaceInvalidation(load);
    await Promise.all([onActivity(), onActivity()]);
    expect(load).toHaveBeenCalledTimes(2);
    expect(preserveFlags).toEqual([true, true]);
  });
});
