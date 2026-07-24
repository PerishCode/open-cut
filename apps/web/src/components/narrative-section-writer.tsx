import { Button, Stack, Status, Text, TextField } from "@open-cut/components";
import {
  type CommitCreatorEditInput,
  type CreatorEditCommit,
  CreatorEditError,
  type DurableID,
  type EditingPorts,
  type NarrativeSection,
  type NarrativeSubtree,
  type RevisionString,
} from "@open-cut/contracts";
import type { KeyboardEvent, ReactNode } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { formatLanguageLabel } from "./creator-workspace-presentation.js";

type AsyncResult = unknown;
type SectionPhase = "clean" | "dirty" | "saving" | "saving-dirty" | "conflict" | "error";
type SectionDraft = Readonly<{ title: string; phase: SectionPhase; error?: Error }>;
type SectionAttempt = Readonly<{
  title: string;
  base: NarrativeSection;
  kind: "title" | "move" | "remove";
  input: CommitCreatorEditInput;
}>;
type ChildState =
  | Readonly<{ status: "idle" | "loading" }>
  | Readonly<{ status: "ready"; value: NarrativeSubtree }>
  | Readonly<{ status: "error"; error: Error }>;

export function CreatorNarrativeSection({
  autoExpand = false,
  autoFocus,
  canMoveDown,
  canMoveUp,
  documentId,
  moveDownAfterNodeId,
  moveUpAfterNodeId,
  onReceipt,
  onReload,
  onSelect,
  parentId,
  parentRevision,
  projectId,
  projectRevision,
  read,
  renderChildren,
  section,
  sequenceId,
  write,
}: Readonly<{
  autoExpand?: boolean;
  autoFocus: boolean;
  canMoveDown: boolean;
  canMoveUp: boolean;
  documentId: DurableID;
  moveDownAfterNodeId?: DurableID;
  moveUpAfterNodeId?: DurableID;
  onReceipt(receipt: CreatorEditCommit, focusId?: DurableID): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  onSelect(): void;
  parentId: DurableID;
  parentRevision: RevisionString;
  projectId: DurableID;
  projectRevision: RevisionString;
  read: EditingPorts["read"];
  renderChildren(value: NarrativeSubtree, onReload: () => Promise<AsyncResult>, focusEmpty: boolean): ReactNode;
  section: NarrativeSection;
  sequenceId: DurableID;
  write: EditingPorts["write"];
}>) {
  const [draft, setDraft] = useState<SectionDraft>({ title: section.title, phase: "clean" });
  const [expanded, setExpanded] = useState(autoExpand);
  const [focusChildParagraph, setFocusChildParagraph] = useState(false);
  const [children, setChildren] = useState<ChildState>({ status: "idle" });
  const titleRef = useRef(section.title);
  const latestSectionRef = useRef(section);
  const baseSectionRef = useRef(section);
  const latestProjectRevisionRef = useRef(projectRevision);
  const baseProjectRevisionRef = useRef(projectRevision);
  const latestParentRevisionRef = useRef(parentRevision);
  const baseParentRevisionRef = useRef(parentRevision);
  const attemptRef = useRef<SectionAttempt | undefined>(undefined);
  const inFlightRef = useRef(false);

  useEffect(() => {
    latestSectionRef.current = section;
    latestProjectRevisionRef.current = projectRevision;
    latestParentRevisionRef.current = parentRevision;
    setDraft((current) => {
      if (current.phase !== "clean") return current;
      baseSectionRef.current = section;
      baseProjectRevisionRef.current = projectRevision;
      baseParentRevisionRef.current = parentRevision;
      titleRef.current = section.title;
      return { title: section.title, phase: "clean" };
    });
  }, [parentRevision, projectRevision, section]);

  const loadChildren = useCallback(async () => {
    setChildren((current) => (current.status === "ready" ? current : { status: "loading" }));
    try {
      const value = await read.narrativeSubtree({ projectId, documentId, parentId: section.id, limit: 200 });
      setChildren({ status: "ready", value });
    } catch (value) {
      setChildren({ status: "error", error: asError(value) });
    }
  }, [documentId, projectId, read, section.id]);

  useEffect(() => {
    if (!autoExpand) return;
    setExpanded(true);
    void loadChildren();
  }, [autoExpand, loadChildren]);

  const reloadChildren = useCallback(async () => {
    await onReload();
    await loadChildren();
  }, [loadChildren, onReload]);

  const runAttempt = useCallback(
    async (attempt: SectionAttempt, expandAfter = false) => {
      if (inFlightRef.current) return;
      attemptRef.current = attempt;
      inFlightRef.current = true;
      setDraft((current) => ({ ...current, phase: current.title === attempt.title ? "saving" : "saving-dirty" }));
      try {
        const receipt = await write.commit(attempt.input);
        const changed = receipt.changes.find(
          (change) => change.kind === "narrative-node" && change.id === attempt.base.id,
        );
        if (!changed) throw new Error("Creator edit receipt omitted the Narrative Section");
        if (attempt.kind === "title") {
          baseSectionRef.current = { ...attempt.base, title: attempt.title, revision: changed.revision };
          baseProjectRevisionRef.current = receipt.committedProjectRevision;
          setDraft((current) =>
            current.title === attempt.title
              ? { title: attempt.title, phase: "clean" }
              : { ...current, phase: "dirty", error: undefined },
          );
        }
        await onReceipt(receipt, attempt.kind === "move" ? attempt.base.id : undefined);
        attemptRef.current = undefined;
        if (expandAfter) {
          setExpanded(true);
          setFocusChildParagraph(true);
          await loadChildren();
        }
      } catch (value) {
        const error = asError(value);
        setDraft((current) => ({ ...current, phase: isConflict(error) ? "conflict" : "error", error }));
      } finally {
        inFlightRef.current = false;
      }
    },
    [loadChildren, onReceipt, write],
  );

  const checkpoint = useCallback(
    async (expandAfter = false) => {
      const title = titleRef.current;
      const base = baseSectionRef.current;
      if (inFlightRef.current || title.trim().length === 0) return;
      if (title === base.title) {
        if (expandAfter) {
          setExpanded(true);
          setFocusChildParagraph(true);
          await loadChildren();
        }
        return;
      }
      const prior = attemptRef.current;
      const attempt: SectionAttempt =
        prior?.kind === "title" && prior.title === title
          ? prior
          : {
              title,
              base,
              kind: "title" as const,
              input: {
                projectId,
                sequenceId,
                requestId: sectionRequestID("title"),
                intent: `Rename Narrative Section ${base.id}`,
                baseProjectRevision: baseProjectRevisionRef.current,
                preconditions: [{ kind: "narrative-node", id: base.id, revision: base.revision }],
                operations: [
                  {
                    type: "update-section" as const,
                    nodeId: base.id,
                    title,
                    language: base.language,
                  },
                ],
              },
            };
      await runAttempt(attempt, expandAfter);
    },
    [loadChildren, projectId, runAttempt, sequenceId],
  );

  useEffect(() => {
    if (draft.phase !== "dirty" || draft.title.trim().length === 0) return;
    const timer = setTimeout(() => void checkpoint(), 750);
    return () => clearTimeout(timer);
  }, [checkpoint, draft.phase, draft.title]);

  const move = useCallback(
    (afterNodeId?: DurableID) => {
      const base = baseSectionRef.current;
      if (draft.phase !== "clean" || inFlightRef.current) return;
      void runAttempt({
        title: titleRef.current,
        base,
        kind: "move",
        input: {
          projectId,
          sequenceId,
          requestId: sectionRequestID("move"),
          intent: `Move Narrative Section ${base.id}`,
          baseProjectRevision: baseProjectRevisionRef.current,
          preconditions: [
            { kind: "narrative-node", id: base.id, revision: base.revision },
            { kind: "narrative-node", id: parentId, revision: baseParentRevisionRef.current },
          ],
          operations: [
            {
              type: "move-narrative-node",
              nodeId: base.id,
              parentId,
              ...(afterNodeId === undefined ? {} : { afterNodeId }),
            },
          ],
        },
      });
    },
    [draft.phase, parentId, projectId, runAttempt, sequenceId],
  );

  const canRemove =
    draft.phase === "clean" &&
    children.status === "ready" &&
    children.value.nodes.length === 0 &&
    children.value.nextAfter === undefined;
  const remove = useCallback(() => {
    const base = baseSectionRef.current;
    if (!canRemove || inFlightRef.current) return;
    void runAttempt({
      title: titleRef.current,
      base,
      kind: "remove",
      input: {
        projectId,
        sequenceId,
        requestId: sectionRequestID("remove"),
        intent: `Remove empty Narrative Section ${base.id}`,
        baseProjectRevision: baseProjectRevisionRef.current,
        preconditions: [
          { kind: "narrative-node", id: base.id, revision: base.revision },
          { kind: "narrative-node", id: parentId, revision: baseParentRevisionRef.current },
        ],
        operations: [{ type: "remove-narrative-node", nodeId: base.id }],
      },
    });
  }, [canRemove, parentId, projectId, runAttempt, sequenceId]);

  const refreshForRetry = useCallback(async () => {
    const prior = attemptRef.current;
    await onReload();
    baseSectionRef.current = latestSectionRef.current;
    baseProjectRevisionRef.current = latestProjectRevisionRef.current;
    baseParentRevisionRef.current = latestParentRevisionRef.current;
    if (!prior) {
      setDraft((current) => ({ ...current, phase: "dirty", error: undefined }));
      return;
    }
    const refreshed: SectionAttempt = {
      ...prior,
      base: latestSectionRef.current,
      input: {
        ...prior.input,
        requestId: sectionRequestID("refresh"),
        baseProjectRevision: latestProjectRevisionRef.current,
        preconditions: prior.input.preconditions.map((condition) => {
          if (condition.id === section.id) return { ...condition, revision: latestSectionRef.current.revision };
          if (condition.id === parentId) return { ...condition, revision: latestParentRevisionRef.current };
          return condition;
        }),
      },
    };
    await runAttempt(refreshed);
  }, [onReload, parentId, runAttempt, section.id]);

  const reloadTitle = useCallback(async () => {
    await onReload();
    const latest = latestSectionRef.current;
    baseSectionRef.current = latest;
    baseProjectRevisionRef.current = latestProjectRevisionRef.current;
    baseParentRevisionRef.current = latestParentRevisionRef.current;
    titleRef.current = latest.title;
    attemptRef.current = undefined;
    setDraft({ title: latest.title, phase: "clean" });
  }, [onReload]);

  const retryIdentical = useCallback(() => {
    const attempt = attemptRef.current;
    if (attempt) void runAttempt(attempt);
  }, [runAttempt]);

  const handleTitleKey = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (event.key !== "Enter" || event.altKey || event.ctrlKey || event.metaKey || event.nativeEvent.isComposing) {
        return;
      }
      event.preventDefault();
      void checkpoint(true);
    },
    [checkpoint],
  );

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">
        SECTION · {formatLanguageLabel(section.language)} · r{section.revision}
      </Text>
      <TextField
        disabled={inFlightRef.current && attemptRef.current?.kind !== "title"}
        focusRequest={autoFocus ? section.id : undefined}
        label={`Narrative Section ${section.title}`}
        maxLength={262_144}
        onBlur={() => void checkpoint()}
        onChange={(title) => {
          titleRef.current = title;
          setDraft((current) => ({
            title,
            phase: inFlightRef.current ? "saving-dirty" : title === baseSectionRef.current.title ? "clean" : "dirty",
            error: current.error,
          }));
        }}
        onFocus={onSelect}
        onKeyDown={handleTitleKey}
        value={draft.title}
      />
      <Status state={phaseState(draft.phase)}>{phaseText(draft.phase)}</Status>
      <Stack spacing="compact">
        <Button
          onPress={() => {
            if (expanded) {
              setExpanded(false);
              setFocusChildParagraph(false);
            } else {
              setExpanded(true);
              if (children.status !== "ready") void loadChildren();
            }
          }}
        >
          {expanded ? "Collapse Section" : "Expand Section"}
        </Button>
        <Button disabled={!canMoveUp || draft.phase !== "clean"} onPress={() => move(moveUpAfterNodeId)}>
          Move Section up
        </Button>
        <Button disabled={!canMoveDown || draft.phase !== "clean"} onPress={() => move(moveDownAfterNodeId)}>
          Move Section down
        </Button>
        <Button disabled={!canRemove} onPress={() => remove()}>
          Remove empty Section
        </Button>
      </Stack>
      {draft.phase === "error" ? (
        <>
          {attemptRef.current?.title === draft.title ? (
            <Button onPress={retryIdentical}>Retry identical checkpoint</Button>
          ) : null}
          <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
          <Button onPress={() => void reloadTitle()}>Reload committed title</Button>
        </>
      ) : null}
      {draft.phase === "conflict" ? (
        <>
          <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
          <Button onPress={() => void reloadTitle()}>Reload committed title</Button>
        </>
      ) : null}
      {expanded && children.status === "loading" ? <Status state="pending">Loading Section…</Status> : null}
      {expanded && children.status === "error" ? (
        <>
          <Status state="unavailable">Section unavailable · {children.error.message}</Status>
          <Button onPress={() => void loadChildren()}>Retry Section read</Button>
        </>
      ) : null}
      {expanded && children.status === "ready"
        ? renderChildren(children.value, reloadChildren, focusChildParagraph)
        : null}
    </Stack>
  );
}

export function NewNarrativeSection({
  afterNodeId,
  language,
  onReceipt,
  onReload,
  parentId,
  parentRevision,
  projectId,
  projectRevision,
  sequenceId,
  write,
}: Readonly<{
  afterNodeId?: DurableID;
  language: string;
  onReceipt(receipt: CreatorEditCommit, focusId?: DurableID): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  parentId: DurableID;
  parentRevision: RevisionString;
  projectId: DurableID;
  projectRevision: RevisionString;
  sequenceId: DurableID;
  write: EditingPorts["write"];
}>) {
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<SectionDraft>({ title: "", phase: "clean" });
  const titleRef = useRef("");
  const attemptRef = useRef<Readonly<{ title: string; local: string; input: CommitCreatorEditInput }> | undefined>(
    undefined,
  );
  const inFlightRef = useRef(false);

  const checkpoint = useCallback(async () => {
    const title = titleRef.current;
    if (inFlightRef.current || title.trim().length === 0) return;
    let attempt = attemptRef.current?.title === title ? attemptRef.current : undefined;
    if (!attempt) {
      const local = `section_${crypto.randomUUID().replaceAll("-", "")}`;
      attempt = {
        title,
        local,
        input: {
          projectId,
          sequenceId,
          requestId: sectionRequestID("insert"),
          intent: "Add Narrative Section",
          baseProjectRevision: projectRevision,
          preconditions: [{ kind: "narrative-node", id: parentId, revision: parentRevision }],
          operations: [
            {
              type: "insert-section",
              createAs: local,
              parentId,
              ...(afterNodeId === undefined ? {} : { afterNodeId }),
              title,
              language,
            },
          ],
        },
      };
    }
    attemptRef.current = attempt;
    inFlightRef.current = true;
    setDraft((current) => ({ ...current, phase: current.title === title ? "saving" : "saving-dirty" }));
    try {
      const receipt = await write.commit(attempt.input);
      const allocation = receipt.allocation.find((item) => item.local === attempt.local);
      if (!allocation) throw new Error("Creator edit receipt omitted the new Narrative Section");
      attemptRef.current = undefined;
      titleRef.current = "";
      setDraft({ title: "", phase: "clean" });
      setCreating(false);
      await onReceipt(receipt, allocation.id);
    } catch (value) {
      const error = asError(value);
      setDraft((current) => ({ ...current, phase: isConflict(error) ? "conflict" : "error", error }));
    } finally {
      inFlightRef.current = false;
    }
  }, [afterNodeId, language, onReceipt, parentId, parentRevision, projectId, projectRevision, sequenceId, write]);

  useEffect(() => {
    if (draft.phase !== "dirty" || draft.title.trim().length === 0) return;
    const timer = setTimeout(() => void checkpoint(), 750);
    return () => clearTimeout(timer);
  }, [checkpoint, draft.phase, draft.title]);

  const refreshForRetry = useCallback(async () => {
    await onReload();
    attemptRef.current = undefined;
    setDraft((current) => ({ ...current, phase: "dirty", error: undefined }));
  }, [onReload]);

  if (!creating) return <Button onPress={() => setCreating(true)}>Add Section</Button>;
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">NEW SECTION · {formatLanguageLabel(language)}</Text>
      <TextField
        disabled={inFlightRef.current}
        focusRequest="new-section"
        label="New Narrative Section title"
        maxLength={262_144}
        onBlur={() => void checkpoint()}
        onChange={(title) => {
          titleRef.current = title;
          setDraft((current) => ({
            title,
            phase: inFlightRef.current ? "saving-dirty" : title.length === 0 ? "clean" : "dirty",
            error: current.error,
          }));
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
          event.preventDefault();
          void checkpoint();
        }}
        placeholder="Name this section…"
        value={draft.title}
      />
      <Status state={phaseState(draft.phase)}>{phaseText(draft.phase)}</Status>
      {draft.phase === "error" ? (
        <>
          {attemptRef.current?.title === draft.title ? (
            <Button onPress={() => void checkpoint()}>Retry identical checkpoint</Button>
          ) : null}
          <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
        </>
      ) : null}
      {draft.phase === "conflict" ? (
        <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
      ) : null}
    </Stack>
  );
}

function phaseState(phase: SectionPhase): "ready" | "pending" | "unavailable" {
  if (phase === "clean") return "ready";
  if (phase === "conflict" || phase === "error") return "unavailable";
  return "pending";
}

function phaseText(phase: SectionPhase): string {
  if (phase === "clean") return "Committed";
  if (phase === "dirty") return "Unsaved · checkpoints after 750 ms";
  if (phase === "saving") return "Saving checkpoint…";
  if (phase === "saving-dirty") return "Saving older checkpoint · newer title remains unsaved";
  if (phase === "conflict") return "Conflict · local title preserved";
  return "Checkpoint failed · local title preserved";
}

function sectionRequestID(kind: string): string {
  return `ui:creator-section-${kind}:${crypto.randomUUID()}`;
}

function isConflict(value: Error): boolean {
  return value instanceof CreatorEditError && value.code === "conflict";
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
