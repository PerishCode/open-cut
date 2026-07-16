import assert from "node:assert/strict";
import path from "node:path";

import type { WebContents } from "electron";
import { beforeEach, describe, it, vi } from "vitest";

type IPCHandler = (
  event: {
    sender: { id: number; getURL(): string };
    senderFrame?: { url: string };
  },
  sourcePath: unknown,
) => unknown;

const ipc = vi.hoisted(() => {
  const handlers = new Map<string, IPCHandler>();
  return {
    handlers,
    handle: vi.fn((channel: string, handler: IPCHandler) => handlers.set(channel, handler)),
    removeHandler: vi.fn((channel: string) => handlers.delete(channel)),
  };
});

vi.mock("electron", () => ({ ipcMain: ipc }));

import { registerDroppedSourceIPC } from "../src/main/dropped-source-ipc.js";
import { droppedSourceIPCChannel } from "../src/platform-channel.js";

beforeEach(() => {
  ipc.handlers.clear();
  ipc.handle.mockClear();
  ipc.removeHandler.mockClear();
});

describe("dropped source IPC", () => {
  it("accepts only the trusted renderer and binds consumption to its WebContents", () => {
    const droppedSources = registerDroppedSourceIPC();
    const handler = ipc.handlers.get(droppedSourceIPCChannel);
    assert.ok(handler);
    const owner = { id: 7, getURL: () => "oc://app/" };
    const sourcePath = path.resolve("fixture.mov");
    const token = handler({ sender: owner, senderFrame: { url: "oc://app/workspace" } }, sourcePath);
    assert.equal(typeof token, "string");
    assert.equal(droppedSources.consume({ id: 8 } as WebContents, String(token)), undefined);
    assert.equal(droppedSources.consume(owner as WebContents, String(token)), sourcePath);
    assert.throws(
      () => handler({ sender: owner, senderFrame: { url: "https://malicious.invalid/" } }, sourcePath),
      /caller is not trusted/,
    );

    droppedSources.close();
    assert.equal(ipc.handlers.has(droppedSourceIPCChannel), false);
  });
});
