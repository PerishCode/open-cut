import { Fragment, type ReactNode } from "react";

import styles from "./message-content.module.css";

export type MessageContentProps = Readonly<{
  text: string;
}>;

type MessageBlock =
  | Readonly<{ kind: "code"; language?: string; text: string }>
  | Readonly<{ kind: "ordered-list"; items: readonly string[] }>
  | Readonly<{ kind: "paragraph"; lines: readonly string[] }>
  | Readonly<{ kind: "unordered-list"; items: readonly string[] }>;

const fencePattern = /^ {0,3}```([^`\s]*)\s*$/;
const closingFencePattern = /^ {0,3}```\s*$/;
const orderedItemPattern = /^ {0,3}\d+[.)]\s+(.+)$/;
const unorderedItemPattern = /^ {0,3}[-*+]\s+(.+)$/;

/**
 * Safely presents a deliberately small Markdown-like subset for conversational prose.
 * Source text remains the durable truth; HTML, links, images, and arbitrary attributes
 * are never interpreted.
 */
export function MessageContent({ text }: MessageContentProps) {
  return (
    <div className={styles.messageContent}>{parseBlocks(text).map((block, index) => renderBlock(block, index))}</div>
  );
}

function parseBlocks(source: string): readonly MessageBlock[] {
  const lines = source.replaceAll("\r\n", "\n").replaceAll("\r", "\n").split("\n");
  const blocks: MessageBlock[] = [];
  let cursor = 0;

  while (cursor < lines.length) {
    const line = lines[cursor] ?? "";
    if (line.trim() === "") {
      cursor += 1;
      continue;
    }

    const fence = line.match(fencePattern);
    if (fence) {
      const code: string[] = [];
      cursor += 1;
      while (cursor < lines.length && !closingFencePattern.test(lines[cursor] ?? "")) {
        code.push(lines[cursor] ?? "");
        cursor += 1;
      }
      if (cursor < lines.length) cursor += 1;
      blocks.push({
        kind: "code",
        language: fence[1] || undefined,
        text: code.join("\n"),
      });
      continue;
    }

    const ordered = line.match(orderedItemPattern);
    if (ordered) {
      const items: string[] = [];
      while (cursor < lines.length) {
        const item = (lines[cursor] ?? "").match(orderedItemPattern);
        if (!item) break;
        items.push(item[1] ?? "");
        cursor += 1;
      }
      blocks.push({ kind: "ordered-list", items });
      continue;
    }

    const unordered = line.match(unorderedItemPattern);
    if (unordered) {
      const items: string[] = [];
      while (cursor < lines.length) {
        const item = (lines[cursor] ?? "").match(unorderedItemPattern);
        if (!item) break;
        items.push(item[1] ?? "");
        cursor += 1;
      }
      blocks.push({ kind: "unordered-list", items });
      continue;
    }

    const paragraph: string[] = [];
    while (cursor < lines.length) {
      const candidate = lines[cursor] ?? "";
      if (
        candidate.trim() === "" ||
        fencePattern.test(candidate) ||
        orderedItemPattern.test(candidate) ||
        unorderedItemPattern.test(candidate)
      ) {
        break;
      }
      paragraph.push(candidate);
      cursor += 1;
    }
    blocks.push({ kind: "paragraph", lines: paragraph });
  }

  return blocks;
}

function renderBlock(block: MessageBlock, index: number): ReactNode {
  const key = `${block.kind}:${index}`;
  if (block.kind === "code") {
    return (
      <pre data-language={block.language} key={key}>
        <code>{block.text}</code>
      </pre>
    );
  }
  if (block.kind === "ordered-list") {
    return (
      <ol key={key}>
        {block.items.map((item, itemIndex) => (
          <li key={`${key}:${itemIndex}`}>{renderInline(item, `${key}:${itemIndex}`)}</li>
        ))}
      </ol>
    );
  }
  if (block.kind === "unordered-list") {
    return (
      <ul key={key}>
        {block.items.map((item, itemIndex) => (
          <li key={`${key}:${itemIndex}`}>{renderInline(item, `${key}:${itemIndex}`)}</li>
        ))}
      </ul>
    );
  }
  return (
    <p key={key}>
      {block.lines.map((line, lineIndex) => (
        <Fragment key={`${key}:${lineIndex}`}>
          {lineIndex > 0 ? <br /> : null}
          {renderInline(line, `${key}:${lineIndex}`)}
        </Fragment>
      ))}
    </p>
  );
}

function renderInline(source: string, keyPrefix: string): readonly ReactNode[] {
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let codeIndex = 0;

  while (cursor < source.length) {
    const opening = source.indexOf("`", cursor);
    if (opening < 0) {
      nodes.push(source.slice(cursor));
      break;
    }
    const closing = source.indexOf("`", opening + 1);
    if (closing < 0) {
      nodes.push(source.slice(cursor));
      break;
    }
    if (opening > cursor) nodes.push(source.slice(cursor, opening));
    if (closing === opening + 1) {
      nodes.push("`");
      cursor = opening + 1;
      continue;
    }
    nodes.push(<code key={`${keyPrefix}:code:${codeIndex}`}>{source.slice(opening + 1, closing)}</code>);
    codeIndex += 1;
    cursor = closing + 1;
  }

  return nodes;
}
