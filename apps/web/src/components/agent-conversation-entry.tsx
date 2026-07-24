import { FeedEntry, MessageContent, Text } from "@open-cut/components";
import type { AgentContextAttachment, AgentConversationMessage } from "@open-cut/contracts";
import type { Ref } from "react";

import { formatClock, formatClockEnd } from "./creator-workspace-presentation.js";

export function AgentConversationEntry({
  elementRef,
  message,
}: Readonly<{
  elementRef?: Ref<HTMLElement>;
  message: AgentConversationMessage;
}>) {
  const title = messageTitle(message);
  return (
    <FeedEntry
      details={message.attachments.map((attachment) => `@ ${attachmentLabel(attachment)}`)}
      elementRef={elementRef}
      emphasis={message.role === "creator" ? "quiet" : "default"}
      hint={title}
      label={`${title} · message ${message.ordinal}`}
      summary={`${messageRole(message)} · MESSAGE #${message.ordinal}`}
    >
      {message.role === "agent" ? (
        <MessageContent text={presentAgentMessage(message.text)} />
      ) : (
        <Text>
          {message.role === "notice" ? "Agent context was safely rebuilt from this conversation." : message.text}
        </Text>
      )}
    </FeedEntry>
  );
}

function presentAgentMessage(value: string): string {
  return value.replaceAll(
    /\b[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b/gi,
    "[internal reference]",
  );
}

function messageRole(message: AgentConversationMessage): string {
  if (message.role === "creator") return "YOU";
  if (message.role === "agent") return "AGENT";
  return "SYSTEM";
}

function messageTitle(message: AgentConversationMessage): string {
  if (message.role === "creator") return "Your request";
  if (message.role === "agent") return "Agent response";
  return "Context recovery";
}

function attachmentLabel(attachment: AgentContextAttachment): string {
  if ("entity" in attachment) return `${entityKindLabel(attachment.kind)} · r${attachment.entity.revision}`;
  if ("transcript" in attachment) return "Transcript segment";
  if ("point" in attachment) {
    return `Sequence point · ${formatClock(attachment.point.time)} · r${attachment.point.revision}`;
  }
  return `Sequence range · ${formatClock(attachment.range.range.start)} → ${formatClockEnd(
    attachment.range.range,
  )} · r${attachment.range.revision}`;
}

function entityKindLabel(kind: "asset" | "narrative-node" | "clip" | "caption" | "track"): string {
  if (kind === "narrative-node") return "Story";
  return `${kind.slice(0, 1).toUpperCase()}${kind.slice(1)}`;
}
