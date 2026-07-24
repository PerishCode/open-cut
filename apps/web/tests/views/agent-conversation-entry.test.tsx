// @vitest-environment jsdom

import { cursorString, durableID } from "@open-cut/contracts";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AgentConversationEntry } from "../../src/components/agent-conversation-entry.js";

afterEach(cleanup);

describe("AgentConversationEntry", () => {
  it("keeps internal durable references out of Creator-facing Agent prose", () => {
    const internal = "019f8d9e-84be-7552-ae75-c8045201985c";
    render(
      <AgentConversationEntry
        message={{
          id: durableID("019f8d9e-84be-7552-ae75-c80452019851"),
          projectId: durableID("019f8d9e-84be-7552-ae75-c80452019852"),
          runId: durableID("019f8d9e-84be-7552-ae75-c80452019853"),
          turnId: durableID("019f8d9e-84be-7552-ae75-c80452019854"),
          ordinal: cursorString("3"),
          role: "agent",
          text: `Creative change committed. Transaction: ${internal}.`,
          attachments: [],
          createdAt: "2026-07-24T00:00:00Z",
        }}
      />,
    );

    expect(screen.queryByText(new RegExp(internal))).toBeNull();
    expect(screen.getByText(/Transaction: \[internal reference\]\./)).toBeTruthy();
  });
});
