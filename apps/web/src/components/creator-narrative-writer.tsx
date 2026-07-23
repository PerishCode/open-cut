import { Button, Stack, Status, Text, TextAreaField } from "@open-cut/components";
import {
  type AuthoredText,
  type CommitCreatorEditInput,
  type CreatorEditCommit,
  CreatorEditError,
  type DurableID,
  type EditingPorts,
  type NarrativeNode,
  type NarrativeSubtree,
  type RevisionString,
  type SourceExcerpt,
  useContracts,
} from "@open-cut/contracts";
import { Fragment, type KeyboardEvent, useCallback, useEffect, useRef, useState } from "react";
import type { NarrativeInsertionAnchor } from "./creator-narrative-anchor.js";
import { narrativeNodeID, narrativeNodeLabel, narrativeNodeText } from "./creator-workspace-presentation.js";
import { CreatorNarrativeSection, NewNarrativeSection } from "./narrative-section-writer.js";
import { NewNarrativeParagraph } from "./new-narrative-paragraph.js";

type DraftPhase = "clean" | "dirty" | "saving" | "saving-dirty" | "conflict" | "error";
type AsyncResult = unknown;
type TextValue = string;
type PromotedDrafts = Readonly<Record<TextValue, TextValue>>;
type DraftPhases = Readonly<Record<TextValue, DraftPhase>>;
type UpdateAttempt = Readonly<{
  value: string;
  base: AuthoredText;
  input: CommitCreatorEditInput;
}>;
type StructuralAttempt = Readonly<{
  visibleValue: string;
  settledValue: string;
  input: CommitCreatorEditInput;
  allocationLocal?: string;
  focusId?: DurableID;
}>;

type DraftState = Readonly<{
  value: string;
  phase: DraftPhase;
  error?: Error;
}>;

type WriterProps = Readonly<{
  activeSectionPath?: readonly DurableID[];
  projectId: DurableID;
  sequenceId: DurableID;
  projectRevision: RevisionString;
  narrative: NarrativeSubtree;
  focusEmpty?: boolean;
  onCommitReceipt?(receipt: CreatorEditCommit): void;
  onAddToRoughCut?(sourceExcerpt: SourceExcerpt, evidenceStatus: "exact" | "stale"): void;
  onCreateCaptions?(sourceExcerpt: SourceExcerpt, evidenceStatus: "exact" | "stale"): void;
  onReload(): Promise<AsyncResult>;
  onSelect(node: NarrativeNode, anchor: NarrativeInsertionAnchor, sectionPath: readonly DurableID[]): void;
  recentlyAddedNodeId?: DurableID;
  sectionPath?: readonly DurableID[];
}>;

export function CreatorNarrativeWriter({
  activeSectionPath,
  projectId,
  sequenceId,
  projectRevision,
  narrative,
  focusEmpty = false,
  onCommitReceipt,
  onAddToRoughCut,
  onCreateCaptions,
  onReload,
  onSelect,
  recentlyAddedNodeId,
  sectionPath = [],
}: WriterProps) {
  const contracts = useContracts();
  const [promotedDrafts, setPromotedDrafts] = useState<PromotedDrafts>({});
  const [draftPhases, setDraftPhases] = useState<DraftPhases>({});
  const lastNode = narrative.nodes.at(-1);
  const [emptyAfterNodeId, setEmptyAfterNodeId] = useState<DurableID | null>(() =>
    lastNode ? narrativeNodeID(lastNode) : null,
  );
  const [emptyEditorVersion, setEmptyEditorVersion] = useState(0);
  const [emptyDraftPhase, setEmptyDraftPhase] = useState<DraftPhase>("clean");
  const [focusParagraphId, setFocusParagraphId] = useState<DurableID>();

  const recordReceipt = useCallback((receipt: CreatorEditCommit) => onCommitReceipt?.(receipt), [onCommitReceipt]);

  const acceptReceipt = useCallback(
    async (receipt: CreatorEditCommit, focusId?: DurableID) => {
      recordReceipt(receipt);
      setFocusParagraphId(focusId);
      await onReload();
    },
    [onReload, recordReceipt],
  );

  const relocateEmptyEditor = useCallback(
    (afterNodeId?: DurableID) => {
      if (emptyDraftPhase !== "clean") return false;
      setEmptyAfterNodeId(afterNodeId ?? null);
      setEmptyEditorVersion((current) => current + 1);
      return true;
    },
    [emptyDraftPhase],
  );

  const emptyParagraph = (
    <NewNarrativeParagraph
      afterNodeId={emptyAfterNodeId ?? undefined}
      autoFocus={focusEmpty || emptyEditorVersion > 0}
      key={emptyEditorVersion}
      language={narrative.parent.language}
      onPhaseChange={setEmptyDraftPhase}
      onReload={onReload}
      onPromote={(id, value) => setPromotedDrafts((current) => ({ ...current, [id]: value }))}
      onReceipt={async (receipt, insertedId) => {
        setEmptyAfterNodeId(insertedId);
        setEmptyEditorVersion((current) => current + 1);
        await acceptReceipt(receipt, insertedId);
      }}
      parentId={narrative.parent.id}
      parentRevision={narrative.parent.revision}
      projectId={projectId}
      projectRevision={projectRevision}
      sequenceId={sequenceId}
      write={contracts.editing.write}
    />
  );
  const anchorIsLoaded =
    emptyAfterNodeId === null || narrative.nodes.some((node) => narrativeNodeID(node) === emptyAfterNodeId);
  const selectNode = useCallback(
    (node: NarrativeNode) =>
      onSelect(
        node,
        {
          parentId: narrative.parent.id,
          parentRevision: narrative.parent.revision,
          afterNodeId: narrativeNodeID(node),
          label: `after ${narrativeNodeLabel(node)}`,
        },
        sectionPath,
      ),
    [narrative.parent.id, narrative.parent.revision, onSelect, sectionPath],
  );

  return (
    <Stack spacing="compact">
      {emptyAfterNodeId === null ? emptyParagraph : null}
      {narrative.nodes.map((node, index) => {
        const nodeId = narrativeNodeID(node);
        const previousNode = narrative.nodes[index - 1];
        const moveUpAfterNode = narrative.nodes[index - 2];
        const moveDownAfterNode = narrative.nodes[index + 1];
        return (
          <Fragment key={nodeId}>
            {node.kind === "authored-text" ? (
              <NarrativeParagraphEditor
                autoFocus={focusParagraphId === node.authoredText.id}
                canMoveDown={moveDownAfterNode !== undefined}
                canMoveUp={previousNode !== undefined}
                initialDraft={promotedDrafts[node.authoredText.id]}
                moveDownAfterNodeId={moveDownAfterNode ? narrativeNodeID(moveDownAfterNode) : undefined}
                moveUpAfterNodeId={moveUpAfterNode ? narrativeNodeID(moveUpAfterNode) : undefined}
                node={node.authoredText}
                onAdoptInitialDraft={() =>
                  setPromotedDrafts((current) => {
                    const next = { ...current };
                    delete next[node.authoredText.id];
                    return next;
                  })
                }
                onPhaseChange={(phase) => setDraftPhases((current) => ({ ...current, [node.authoredText.id]: phase }))}
                onReceipt={acceptReceipt}
                onReload={onReload}
                onRequestEmptyAfter={relocateEmptyEditor}
                onSelect={() => selectNode(node)}
                ordinal={index + 1}
                parentId={narrative.parent.id}
                parentRevision={narrative.parent.revision}
                previousNode={previousNode}
                previousNodePhase={
                  previousNode?.kind === "authored-text" ? draftPhases[previousNode.authoredText.id] : undefined
                }
                previousSiblingId={previousNode ? narrativeNodeID(previousNode) : undefined}
                projectId={projectId}
                projectRevision={projectRevision}
                sequenceId={sequenceId}
                write={contracts.editing.write}
              />
            ) : node.kind === "section" ? (
              <CreatorNarrativeSection
                autoExpand={activeSectionPath?.[sectionPath.length] === node.section.id}
                autoFocus={focusParagraphId === node.section.id}
                canMoveDown={moveDownAfterNode !== undefined}
                canMoveUp={previousNode !== undefined}
                documentId={narrative.documentId}
                moveDownAfterNodeId={moveDownAfterNode ? narrativeNodeID(moveDownAfterNode) : undefined}
                moveUpAfterNodeId={moveUpAfterNode ? narrativeNodeID(moveUpAfterNode) : undefined}
                onReceipt={acceptReceipt}
                onReload={onReload}
                onSelect={() => selectNode(node)}
                parentId={narrative.parent.id}
                parentRevision={narrative.parent.revision}
                projectId={projectId}
                projectRevision={projectRevision}
                read={contracts.editing.read}
                renderChildren={(children, reloadChildren, focusChildParagraph) => (
                  <CreatorNarrativeWriter
                    activeSectionPath={activeSectionPath}
                    focusEmpty={focusChildParagraph}
                    narrative={children}
                    onAddToRoughCut={onAddToRoughCut}
                    onCreateCaptions={onCreateCaptions}
                    onCommitReceipt={recordReceipt}
                    onReload={reloadChildren}
                    onSelect={onSelect}
                    projectId={projectId}
                    projectRevision={projectRevision}
                    recentlyAddedNodeId={recentlyAddedNodeId}
                    sectionPath={[...sectionPath, node.section.id]}
                    sequenceId={sequenceId}
                  />
                )}
                section={node.section}
                sequenceId={sequenceId}
                write={contracts.editing.write}
              />
            ) : (
              <Stack spacing="compact">
                {nodeId === recentlyAddedNodeId ? <Status state="ready">Added from Transcript</Status> : null}
                <Text tone="eyebrow">
                  {String(index + 1).padStart(2, "0")} · {narrativeNodeLabel(node)}
                </Text>
                <Text>{narrativeNodeText(node)}</Text>
                <Button onPress={() => selectNode(node)}>
                  {node.kind === "source-excerpt" ? "Select Story excerpt" : "Select Story node"}
                </Button>
                {node.kind === "source-excerpt" ? (
                  <Stack spacing="compact">
                    <Button
                      disabled={node.evidenceStatus !== "exact" || !onAddToRoughCut}
                      onPress={() => onAddToRoughCut?.(node.sourceExcerpt, node.evidenceStatus)}
                    >
                      Add excerpt to rough cut
                    </Button>
                    <Button
                      disabled={node.evidenceStatus !== "exact" || !onCreateCaptions}
                      onPress={() => onCreateCaptions?.(node.sourceExcerpt, node.evidenceStatus)}
                    >
                      Create captions from excerpt
                    </Button>
                  </Stack>
                ) : null}
              </Stack>
            )}
            {emptyAfterNodeId === nodeId ? emptyParagraph : null}
          </Fragment>
        );
      })}
      {!anchorIsLoaded ? emptyParagraph : null}
      <NewNarrativeSection
        afterNodeId={lastNode ? narrativeNodeID(lastNode) : undefined}
        language={narrative.parent.language}
        onReceipt={acceptReceipt}
        onReload={onReload}
        parentId={narrative.parent.id}
        parentRevision={narrative.parent.revision}
        projectId={projectId}
        projectRevision={projectRevision}
        sequenceId={sequenceId}
        write={contracts.editing.write}
      />
    </Stack>
  );
}

function NarrativeParagraphEditor({
  autoFocus,
  canMoveDown,
  canMoveUp,
  initialDraft,
  moveDownAfterNodeId,
  moveUpAfterNodeId,
  node,
  onAdoptInitialDraft,
  onPhaseChange,
  onReceipt,
  onReload,
  onRequestEmptyAfter,
  onSelect,
  ordinal,
  parentId,
  parentRevision,
  previousNode,
  previousNodePhase,
  previousSiblingId,
  projectId,
  projectRevision,
  sequenceId,
  write,
}: Readonly<{
  autoFocus: boolean;
  canMoveDown: boolean;
  canMoveUp: boolean;
  initialDraft?: string;
  moveDownAfterNodeId?: DurableID;
  moveUpAfterNodeId?: DurableID;
  node: AuthoredText;
  onAdoptInitialDraft(): void;
  onPhaseChange(phase: DraftPhase): void;
  onReceipt(receipt: CreatorEditCommit, focusId?: DurableID): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  onRequestEmptyAfter(afterNodeId?: DurableID): boolean;
  onSelect(): void;
  ordinal: number;
  parentId: DurableID;
  parentRevision: RevisionString;
  previousNode?: NarrativeNode;
  previousNodePhase?: DraftPhase;
  previousSiblingId?: DurableID;
  projectId: DurableID;
  projectRevision: RevisionString;
  sequenceId: DurableID;
  write: EditingPorts["write"];
}>) {
  const initialValue = initialDraft ?? node.text;
  const previousAuthoredText = previousNode?.kind === "authored-text" ? previousNode.authoredText : undefined;
  const [draft, setDraft] = useState<DraftState>({
    value: initialValue,
    phase: initialValue === node.text ? "clean" : "dirty",
  });
  const draftRef = useRef(initialValue);
  const latestNodeRef = useRef(node);
  const baseNodeRef = useRef(node);
  const latestProjectRevisionRef = useRef(projectRevision);
  const baseProjectRevisionRef = useRef(projectRevision);
  const latestParentRevisionRef = useRef(parentRevision);
  const baseParentRevisionRef = useRef(parentRevision);
  const inFlightRef = useRef(false);
  const attemptRef = useRef<UpdateAttempt | undefined>(undefined);
  const structuralAttemptRef = useRef<StructuralAttempt | undefined>(undefined);
  const [structuralSaving, setStructuralSaving] = useState(false);
  const adoptedRef = useRef(false);
  const onPhaseChangeRef = useRef(onPhaseChange);
  const latestPreviousAuthoredTextRef = useRef(previousAuthoredText);
  onPhaseChangeRef.current = onPhaseChange;
  latestPreviousAuthoredTextRef.current = previousAuthoredText;

  useEffect(() => {
    latestNodeRef.current = node;
    latestProjectRevisionRef.current = projectRevision;
    latestParentRevisionRef.current = parentRevision;
    setDraft((current) => {
      if (current.phase !== "clean") return current;
      baseNodeRef.current = node;
      baseProjectRevisionRef.current = projectRevision;
      baseParentRevisionRef.current = parentRevision;
      draftRef.current = node.text;
      return { value: node.text, phase: "clean" };
    });
  }, [node, parentRevision, projectRevision]);

  useEffect(() => onPhaseChangeRef.current(draft.phase), [draft.phase]);

  useEffect(() => {
    if (initialDraft === undefined || adoptedRef.current) return;
    adoptedRef.current = true;
    onAdoptInitialDraft();
  }, [initialDraft, onAdoptInitialDraft]);

  const checkpoint = useCallback(async () => {
    const value = draftRef.current;
    const base = baseNodeRef.current;
    if (inFlightRef.current || value === base.text || value.length === 0) return;
    let attempt = attemptRef.current?.value === value ? attemptRef.current : undefined;
    if (!attempt) {
      const input: CommitCreatorEditInput = {
        projectId,
        sequenceId,
        requestId: creatorEditRequestID("checkpoint"),
        intent: `Update Narrative paragraph ${base.id}`,
        baseProjectRevision: baseProjectRevisionRef.current,
        preconditions: [{ kind: "narrative-node", id: base.id, revision: base.revision }],
        operations: [
          {
            type: "update-authored-text",
            nodeId: base.id,
            purpose: base.purpose,
            language: base.language,
            text: value,
          },
        ],
      };
      attempt = { value, base, input };
    }
    attemptRef.current = attempt;
    inFlightRef.current = true;
    setDraft((current) => ({ ...current, phase: current.value === value ? "saving" : "saving-dirty" }));
    try {
      const receipt = await write.commit(attempt.input);
      const changed = receipt.changes.find(
        (change) => change.kind === "narrative-node" && change.id === attempt.base.id,
      );
      if (!changed) throw new Error("Creator edit receipt omitted the Narrative paragraph");
      baseNodeRef.current = { ...attempt.base, text: value, revision: changed.revision };
      baseProjectRevisionRef.current = receipt.committedProjectRevision;
      attemptRef.current = undefined;
      setDraft((current) =>
        current.value === value ? { value, phase: "clean" } : { ...current, phase: "dirty", error: undefined },
      );
      await onReceipt(receipt);
    } catch (value) {
      const error = asError(value);
      setDraft((current) => ({ ...current, phase: isConflict(error) ? "conflict" : "error", error }));
    } finally {
      inFlightRef.current = false;
      if (draftRef.current !== baseNodeRef.current.text) {
        setDraft((current) =>
          current.phase === "saving" || current.phase === "saving-dirty" ? { ...current, phase: "dirty" } : current,
        );
      }
    }
  }, [onReceipt, projectId, sequenceId, write]);

  const commitStructure = useCallback(
    async (attempt: StructuralAttempt) => {
      if (inFlightRef.current) return;
      structuralAttemptRef.current = attempt;
      inFlightRef.current = true;
      setStructuralSaving(true);
      setDraft((current) => ({ ...current, phase: "saving", error: undefined }));
      try {
        const receipt = await write.commit(attempt.input);
        const allocatedId = attempt.allocationLocal
          ? receipt.allocation.find((item) => item.local === attempt.allocationLocal)?.id
          : undefined;
        if (attempt.allocationLocal && !allocatedId) {
          throw new Error("Creator edit receipt omitted the split Narrative paragraph");
        }
        draftRef.current = attempt.settledValue;
        setDraft({ value: attempt.settledValue, phase: "clean" });
        await onReceipt(receipt, allocatedId ?? attempt.focusId);
        structuralAttemptRef.current = undefined;
      } catch (value) {
        const error = asError(value);
        draftRef.current = attempt.visibleValue;
        setDraft({
          value: attempt.visibleValue,
          phase: isConflict(error) ? "conflict" : "error",
          error,
        });
      } finally {
        inFlightRef.current = false;
        setStructuralSaving(false);
      }
    },
    [onReceipt, write],
  );

  const splitParagraph = useCallback(
    (left: string, right: string) => {
      if (left.length === 0 || right.length === 0 || inFlightRef.current) return;
      const base = baseNodeRef.current;
      const local = `paragraph_${crypto.randomUUID().replaceAll("-", "")}`;
      void commitStructure({
        visibleValue: draftRef.current,
        settledValue: left,
        allocationLocal: local,
        input: {
          projectId,
          sequenceId,
          requestId: creatorEditRequestID("split"),
          intent: `Split Narrative paragraph ${base.id}`,
          baseProjectRevision: baseProjectRevisionRef.current,
          preconditions: [
            { kind: "narrative-node", id: base.id, revision: base.revision },
            { kind: "narrative-node", id: parentId, revision: baseParentRevisionRef.current },
          ],
          operations: [
            {
              type: "update-authored-text",
              nodeId: base.id,
              purpose: base.purpose,
              language: base.language,
              text: left,
            },
            {
              type: "insert-authored-text",
              createAs: local,
              parentId,
              afterNodeId: base.id,
              purpose: base.purpose,
              language: base.language,
              text: right,
            },
          ],
        },
      });
    },
    [commitStructure, parentId, projectId, sequenceId],
  );

  const canMerge =
    previousAuthoredText !== undefined &&
    (previousNodePhase === undefined || previousNodePhase === "clean") &&
    previousAuthoredText.purpose === baseNodeRef.current.purpose &&
    previousAuthoredText.language === baseNodeRef.current.language;

  const mergeWithPrevious = useCallback(() => {
    const current = baseNodeRef.current;
    if (!previousAuthoredText || !canMerge || inFlightRef.current) return;
    const merged = previousAuthoredText.text + draftRef.current;
    void commitStructure({
      visibleValue: draftRef.current,
      settledValue: draftRef.current,
      focusId: previousAuthoredText.id,
      input: {
        projectId,
        sequenceId,
        requestId: creatorEditRequestID("merge"),
        intent: `Merge Narrative paragraph ${current.id} into ${previousAuthoredText.id}`,
        baseProjectRevision: baseProjectRevisionRef.current,
        preconditions: [
          { kind: "narrative-node", id: previousAuthoredText.id, revision: previousAuthoredText.revision },
          { kind: "narrative-node", id: current.id, revision: current.revision },
          { kind: "narrative-node", id: parentId, revision: baseParentRevisionRef.current },
        ],
        operations: [
          {
            type: "update-authored-text",
            nodeId: previousAuthoredText.id,
            purpose: previousAuthoredText.purpose,
            language: previousAuthoredText.language,
            text: merged,
          },
          { type: "remove-narrative-node", nodeId: current.id },
        ],
      },
    });
  }, [canMerge, commitStructure, parentId, previousAuthoredText, projectId, sequenceId]);

  const moveParagraph = useCallback(
    (afterNodeId?: DurableID) => {
      const base = baseNodeRef.current;
      if (draft.phase !== "clean" || inFlightRef.current) return;
      void commitStructure({
        visibleValue: draftRef.current,
        settledValue: draftRef.current,
        focusId: base.id,
        input: {
          projectId,
          sequenceId,
          requestId: creatorEditRequestID("move"),
          intent: `Move Narrative paragraph ${base.id}`,
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
    [commitStructure, draft.phase, parentId, projectId, sequenceId],
  );

  const removeParagraph = useCallback(() => {
    const base = baseNodeRef.current;
    if (draft.phase !== "clean" || inFlightRef.current) return;
    void commitStructure({
      visibleValue: draftRef.current,
      settledValue: draftRef.current,
      input: {
        projectId,
        sequenceId,
        requestId: creatorEditRequestID("remove"),
        intent: `Remove Narrative paragraph ${base.id}`,
        baseProjectRevision: baseProjectRevisionRef.current,
        preconditions: [
          { kind: "narrative-node", id: base.id, revision: base.revision },
          { kind: "narrative-node", id: parentId, revision: baseParentRevisionRef.current },
        ],
        operations: [{ type: "remove-narrative-node", nodeId: base.id }],
      },
    });
  }, [commitStructure, draft.phase, parentId, projectId, sequenceId]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTextAreaElement>) => {
      if (event.altKey || event.ctrlKey || event.metaKey || event.nativeEvent.isComposing) return;
      const start = event.currentTarget.selectionStart;
      const end = event.currentTarget.selectionEnd;
      if (event.key === "Backspace" && start === 0 && end === 0 && canMerge) {
        event.preventDefault();
        mergeWithPrevious();
        return;
      }
      if (event.key !== "Enter" || event.shiftKey) return;
      const value = draftRef.current;
      if (start === 0 && end === 0) {
        if (onRequestEmptyAfter(previousSiblingId)) event.preventDefault();
        return;
      }
      if (start === value.length && end === value.length) {
        if (onRequestEmptyAfter(baseNodeRef.current.id)) event.preventDefault();
        return;
      }
      const left = value.slice(0, start);
      const right = value.slice(end);
      if (left.length === 0 || right.length === 0) return;
      event.preventDefault();
      splitParagraph(left, right);
    },
    [canMerge, mergeWithPrevious, onRequestEmptyAfter, previousSiblingId, splitParagraph],
  );

  useIdleCheckpoint(draft.phase, draft.value, checkpoint);

  const reload = useCallback(async () => {
    await onReload();
    const latest = latestNodeRef.current;
    baseNodeRef.current = latest;
    baseProjectRevisionRef.current = latestProjectRevisionRef.current;
    baseParentRevisionRef.current = latestParentRevisionRef.current;
    draftRef.current = latest.text;
    attemptRef.current = undefined;
    structuralAttemptRef.current = undefined;
    setDraft({ value: latest.text, phase: "clean" });
  }, [onReload]);

  const refreshForRetry = useCallback(async () => {
    const structuralAttempt = structuralAttemptRef.current;
    await onReload();
    baseNodeRef.current = latestNodeRef.current;
    baseProjectRevisionRef.current = latestProjectRevisionRef.current;
    baseParentRevisionRef.current = latestParentRevisionRef.current;
    attemptRef.current = undefined;
    if (structuralAttempt) {
      const latestPrevious = latestPreviousAuthoredTextRef.current;
      const refreshedAttempt: StructuralAttempt = {
        ...structuralAttempt,
        input: {
          ...structuralAttempt.input,
          requestId: creatorEditRequestID("structure-refresh"),
          baseProjectRevision: latestProjectRevisionRef.current,
          preconditions: structuralAttempt.input.preconditions.map((condition) => {
            if (condition.id === latestNodeRef.current.id) {
              return { ...condition, revision: latestNodeRef.current.revision };
            }
            if (condition.id === parentId) {
              return { ...condition, revision: latestParentRevisionRef.current };
            }
            if (latestPrevious && condition.id === latestPrevious.id) {
              return { ...condition, revision: latestPrevious.revision };
            }
            return condition;
          }),
        },
      };
      structuralAttemptRef.current = refreshedAttempt;
      await commitStructure(refreshedAttempt);
      return;
    }
    structuralAttemptRef.current = undefined;
    setDraft((current) => ({ ...current, phase: "dirty", error: undefined }));
  }, [commitStructure, onReload, parentId]);

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">
        {String(ordinal).padStart(2, "0")} · {node.purpose.toUpperCase()} · {node.language} · r{node.revision}
      </Text>
      <TextAreaField
        disabled={structuralSaving}
        focusRequest={autoFocus ? node.id : undefined}
        label={`Narrative paragraph ${ordinal}`}
        maxLength={262_144}
        onBlur={() => void checkpoint()}
        onChange={(value) => {
          draftRef.current = value;
          setDraft((current) => ({
            value,
            phase: inFlightRef.current ? "saving-dirty" : value === baseNodeRef.current.text ? "clean" : "dirty",
            error: current.error,
          }));
        }}
        onFocus={onSelect}
        onKeyDown={handleKeyDown}
        rows={5}
        value={draft.value}
      />
      <Status state={draftStatusState(draft.phase)}>{draftStatusText(draft.phase)}</Status>
      <Stack spacing="compact">
        <Button disabled={!canMoveUp || draft.phase !== "clean"} onPress={() => void moveParagraph(moveUpAfterNodeId)}>
          Move paragraph up
        </Button>
        <Button
          disabled={!canMoveDown || draft.phase !== "clean"}
          onPress={() => void moveParagraph(moveDownAfterNodeId)}
        >
          Move paragraph down
        </Button>
        <Button disabled={draft.phase !== "clean"} onPress={() => void removeParagraph()}>
          Remove paragraph
        </Button>
      </Stack>
      {draft.phase === "error" ? (
        <>
          {structuralAttemptRef.current?.visibleValue === draft.value || attemptRef.current?.value === draft.value ? (
            <Button
              onPress={() =>
                void (structuralAttemptRef.current ? commitStructure(structuralAttemptRef.current) : checkpoint())
              }
            >
              Retry identical checkpoint
            </Button>
          ) : null}
          <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
          <Button onPress={() => void reload()}>Reload committed text</Button>
        </>
      ) : null}
      {draft.phase === "conflict" ? (
        <>
          <Button onPress={() => void refreshForRetry()}>Refresh revisions for retry</Button>
          <Button onPress={() => void reload()}>Reload committed text</Button>
        </>
      ) : null}
    </Stack>
  );
}

function useIdleCheckpoint(phase: DraftPhase, value: string, checkpoint: () => Promise<AsyncResult>): void {
  useEffect(() => {
    if (phase !== "dirty" || value.length === 0) return;
    const timer = setTimeout(() => void checkpoint(), 750);
    return () => clearTimeout(timer);
  }, [checkpoint, phase, value]);
}

function draftStatusState(phase: DraftPhase): "ready" | "pending" | "unavailable" {
  if (phase === "clean") return "ready";
  if (phase === "conflict" || phase === "error") return "unavailable";
  return "pending";
}

function draftStatusText(phase: DraftPhase): string {
  if (phase === "clean") return "Committed";
  if (phase === "dirty") return "Unsaved · checkpoints after 750 ms";
  if (phase === "saving") return "Saving checkpoint…";
  if (phase === "saving-dirty") return "Saving older checkpoint · newer text remains unsaved";
  if (phase === "conflict") return "Conflict · local text preserved";
  return "Checkpoint failed · local text preserved";
}

function creatorEditRequestID(kind: string): string {
  return `ui:creator-edit-${kind}:${crypto.randomUUID()}`;
}

function isConflict(value: Error): boolean {
  return value instanceof CreatorEditError && value.code === "conflict";
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
