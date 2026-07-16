import { Button, Stack, Status, Text, TextAreaField } from "@open-cut/components";
import {
  type CommitCreatorEditInput,
  type CreatorEditCommit,
  CreatorEditError,
  type DurableID,
  type EditingPorts,
  type RevisionString,
} from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

export type NewNarrativeDraftPhase = "clean" | "dirty" | "saving" | "saving-dirty" | "conflict" | "error";
type AsyncResult = unknown;
type InsertAttempt = Readonly<{
  value: string;
  local: string;
  input: CommitCreatorEditInput;
}>;
type DraftState = Readonly<{
  value: string;
  phase: NewNarrativeDraftPhase;
  error?: Error;
}>;

export function NewNarrativeParagraph({
  afterNodeId,
  autoFocus,
  language,
  onPhaseChange,
  onPromote,
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
  autoFocus: boolean;
  language: string;
  onPhaseChange(phase: NewNarrativeDraftPhase): void;
  onPromote(id: DurableID, value: string): void;
  onReceipt(receipt: CreatorEditCommit, insertedId: DurableID): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  parentId: DurableID;
  parentRevision: RevisionString;
  projectId: DurableID;
  projectRevision: RevisionString;
  sequenceId: DurableID;
  write: EditingPorts["write"];
}>) {
  const [draft, setDraft] = useState<DraftState>({ value: "", phase: "clean" });
  const draftRef = useRef("");
  const inFlightRef = useRef(false);
  const attemptRef = useRef<InsertAttempt | undefined>(undefined);
  const onPhaseChangeRef = useRef(onPhaseChange);
  onPhaseChangeRef.current = onPhaseChange;

  useEffect(() => onPhaseChangeRef.current(draft.phase), [draft.phase]);

  const checkpoint = useCallback(async () => {
    const value = draftRef.current;
    if (inFlightRef.current || value.length === 0) return;
    let attempt = attemptRef.current?.value === value ? attemptRef.current : undefined;
    if (!attempt) {
      const local = `paragraph_${crypto.randomUUID().replaceAll("-", "")}`;
      attempt = {
        value,
        local,
        input: {
          projectId,
          sequenceId,
          requestId: `ui:creator-edit-insert:${crypto.randomUUID()}`,
          intent: "Add Narrative paragraph",
          baseProjectRevision: projectRevision,
          preconditions: [{ kind: "narrative-node", id: parentId, revision: parentRevision }],
          operations: [
            {
              type: "insert-authored-text",
              createAs: local,
              parentId,
              ...(afterNodeId === undefined ? {} : { afterNodeId }),
              purpose: "spoken",
              language,
              text: value,
            },
          ],
        },
      };
    }
    attemptRef.current = attempt;
    inFlightRef.current = true;
    setDraft((current) => ({ ...current, phase: current.value === value ? "saving" : "saving-dirty" }));
    try {
      const receipt = await write.commit(attempt.input);
      const allocation = receipt.allocation.find((item) => item.local === attempt.local);
      if (!allocation) throw new Error("Creator edit receipt omitted the new Narrative paragraph");
      const newerValue = draftRef.current;
      if (newerValue !== value && newerValue.length > 0) onPromote(allocation.id, newerValue);
      draftRef.current = "";
      attemptRef.current = undefined;
      setDraft({ value: "", phase: "clean" });
      await onReceipt(receipt, allocation.id);
    } catch (value) {
      const error = asError(value);
      setDraft((current) => ({
        ...current,
        phase: isConflict(error) ? "conflict" : "error",
        error,
      }));
    } finally {
      inFlightRef.current = false;
    }
  }, [
    afterNodeId,
    language,
    onPromote,
    onReceipt,
    parentId,
    parentRevision,
    projectId,
    projectRevision,
    sequenceId,
    write,
  ]);

  useEffect(() => {
    if (draft.phase !== "dirty" || draft.value.length === 0) return;
    const timer = setTimeout(() => void checkpoint(), 750);
    return () => clearTimeout(timer);
  }, [checkpoint, draft.phase, draft.value]);

  const refreshForRetry = useCallback(async () => {
    await onReload();
    attemptRef.current = undefined;
    setDraft((current) => ({ ...current, phase: "dirty", error: undefined }));
  }, [onReload]);

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">NEW · SPOKEN · {language}</Text>
      <TextAreaField
        focusRequest={autoFocus ? "new-paragraph" : undefined}
        label="New Narrative paragraph"
        maxLength={262_144}
        onBlur={() => void checkpoint()}
        onChange={(value) => {
          draftRef.current = value;
          setDraft((current) => ({
            value,
            phase: inFlightRef.current ? "saving-dirty" : value.length === 0 ? "clean" : "dirty",
            error: current.error,
          }));
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.shiftKey || draftRef.current.length === 0) return;
          event.preventDefault();
          void checkpoint();
        }}
        placeholder="Write the next passage…"
        rows={5}
        value={draft.value}
      />
      <Status state={draftStatusState(draft.phase)}>{draftStatusText(draft.phase)}</Status>
      {draft.phase === "error" ? (
        <>
          {attemptRef.current?.value === draft.value ? (
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

function draftStatusState(phase: NewNarrativeDraftPhase): "ready" | "pending" | "unavailable" {
  if (phase === "clean") return "ready";
  if (phase === "conflict" || phase === "error") return "unavailable";
  return "pending";
}

function draftStatusText(phase: NewNarrativeDraftPhase): string {
  if (phase === "clean") return "Committed";
  if (phase === "dirty") return "Unsaved · checkpoints after 750 ms";
  if (phase === "saving") return "Saving checkpoint…";
  if (phase === "saving-dirty") return "Saving older checkpoint · newer text remains unsaved";
  if (phase === "conflict") return "Conflict · local text preserved";
  return "Checkpoint failed · local text preserved";
}

function isConflict(value: Error): boolean {
  return value instanceof CreatorEditError && value.code === "conflict";
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
