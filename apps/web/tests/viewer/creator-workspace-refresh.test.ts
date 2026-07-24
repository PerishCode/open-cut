// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  createBackgroundWorkspaceInvalidation,
  useCreatorWorkspaceSync,
  workspaceSyncStatus,
} from "../../src/lib/creator-workspace-refresh.js";

describe("useCreatorWorkspaceSync", () => {
  it("keeps a ready workspace mounted during explicit sync", async () => {
    const load = vi.fn(async () => ({ status: "ready" as const }));
    const { result } = renderHook(() => useCreatorWorkspaceSync(load, true));

    await act(() => result.current.run());

    expect(load).toHaveBeenCalledWith(undefined, true);
    expect(workspaceSyncStatus("ready", result.current)).toEqual({
      state: "ready",
      text: "All changes synced",
    });
  });

  it("turns a failed ready-state sync into bounded recovery feedback", async () => {
    const load = vi.fn(async () => {
      throw new Error("offline");
    });
    const { result } = renderHook(() => useCreatorWorkspaceSync(load, true));

    await act(() => result.current.run());

    expect(result.current.failed).toBe(true);
    expect(workspaceSyncStatus("ready", result.current)).toEqual({
      state: "unavailable",
      text: "Could not sync · current view kept",
    });
  });
});

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
