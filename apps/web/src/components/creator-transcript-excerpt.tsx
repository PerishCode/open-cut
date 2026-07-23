import { Button, Stack, Status, Text, TokenSelection } from "@open-cut/components";
import {
  type Asset,
  type CommitCreatorEditInput,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorSourceExcerptEvidence,
  type DurableID,
  selectSourceExcerptEvidence,
  type TranscriptCorrection,
  type TranscriptReadPage,
  type TranscriptSegment,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { NarrativeInsertionAnchor } from "./creator-narrative-anchor.js";
import { formatTime, formatTimeEnd } from "./creator-workspace-presentation.js";

type AsyncResult = unknown;
type InsertPhase = "idle" | "saving" | "error" | "conflict";
type Selection = Readonly<{ artifactId: DurableID; anchorTokenId: DurableID; focusTokenId: DurableID }>;
type InsertAttempt = Readonly<{
  input: CommitCreatorEditInput;
  local: string;
  anchor: NarrativeInsertionAnchor;
  selectionEpoch: number;
}>;

export type CreatorExcerptTarget = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  projectRevision: CommitCreatorEditInput["baseProjectRevision"];
  anchor: NarrativeInsertionAnchor;
  selectionEpoch: number;
  onCommitReceipt(receipt: CreatorEditCommit): void;
  onReload(): Promise<AsyncResult>;
  onInserted(anchor: NarrativeInsertionAnchor): void;
}>;

export function CreatorTranscriptExcerpt({
  asset,
  corrections,
  onContext,
  page,
  segments,
  target,
}: Readonly<{
  asset: Asset;
  corrections: readonly TranscriptCorrection[];
  onContext(page: TranscriptReadPage, segment: TranscriptSegment): void;
  page: TranscriptReadPage;
  segments: readonly TranscriptSegment[];
  target?: CreatorExcerptTarget;
}>) {
  const contracts = useContracts();
  const [selection, setSelection] = useState<Selection>();
  const [phase, setPhase] = useState<InsertPhase>("idle");
  const [error, setError] = useState<Error>();
  const [notice, setNotice] = useState(undefined as string | undefined);
  const [blockedSelectionEpoch, setBlockedSelectionEpoch] = useState(undefined as number | undefined);
  const attemptRef = useRef<InsertAttempt | undefined>(undefined);
  const inFlightRef = useRef(false);

  useEffect(() => {
    setSelection((current) => (current?.artifactId === page.artifact.id ? current : undefined));
    attemptRef.current = undefined;
    setPhase("idle");
    setError(undefined);
    setNotice(undefined);
    setBlockedSelectionEpoch(undefined);
  }, [page.artifact.id]);

  const evidenceResult = useMemo(() => {
    if (!selection) return {};
    try {
      return {
        evidence: selectSourceExcerptEvidence({
          artifact: page.artifact,
          segments,
          corrections,
          anchorTokenId: selection.anchorTokenId,
          focusTokenId: selection.focusTokenId,
        }),
      };
    } catch (value) {
      return { error: asError(value) };
    }
  }, [corrections, page.artifact, segments, selection]);
  const selectedTokenIds = new Set(evidenceResult.evidence?.selectedTokenIds ?? []);
  const usableTarget = target && target.selectionEpoch !== blockedSelectionEpoch ? target : undefined;

  const commit = useCallback(
    async (retry?: InsertAttempt) => {
      if (inFlightRef.current) return;
      let attempt = retry;
      if (!attempt) {
        const evidence = evidenceResult.evidence;
        const fingerprint = asset.acceptedFingerprint;
        if (!usableTarget || !evidence || !fingerprint) return;
        const local = `excerpt_${crypto.randomUUID().replaceAll("-", "")}`;
        attempt = {
          local,
          anchor: usableTarget.anchor,
          selectionEpoch: usableTarget.selectionEpoch,
          input: {
            projectId: usableTarget.projectId,
            sequenceId: usableTarget.sequenceId,
            requestId: `ui:creator-excerpt-insert:${crypto.randomUUID()}`,
            intent: "Insert exact transcript SourceExcerpt",
            baseProjectRevision: usableTarget.projectRevision,
            preconditions: [
              {
                kind: "narrative-node",
                id: usableTarget.anchor.parentId,
                revision: usableTarget.anchor.parentRevision,
              },
              ...evidence.correctionRevisions.map((correction) => ({
                kind: "transcript-correction" as const,
                id: correction.id,
                revision: correction.revision,
              })),
            ],
            operations: [sourceExcerptOperation(local, usableTarget.anchor, fingerprint, evidence)],
          },
        };
      }
      attemptRef.current = attempt;
      inFlightRef.current = true;
      setPhase("saving");
      setError(undefined);
      setNotice(undefined);
      try {
        const receipt = await contracts.editing.write.commit(attempt.input);
        const allocation = receipt.allocation.find((item) => item.local === attempt.local);
        if (!allocation) throw new Error("Creator edit receipt omitted the SourceExcerpt allocation");
        const parent = attempt.anchor.parentId;
        const parentChange = parent
          ? receipt.changes.find((change) => change.kind === "narrative-node" && change.id === parent)
          : undefined;
        if (!target || !parentChange) throw new Error("Creator edit receipt omitted the Narrative insertion parent");
        const insertedAnchor = {
          parentId: parent,
          parentRevision: parentChange.revision,
          afterNodeId: allocation.id,
          label: "after inserted excerpt",
        } satisfies NarrativeInsertionAnchor;
        target.onCommitReceipt(receipt);
        attemptRef.current = undefined;
        setSelection(undefined);
        setPhase("idle");
        setNotice("Excerpt added to Story");
        try {
          await target.onReload();
          target.onInserted(insertedAnchor);
        } catch {
          setNotice("Excerpt added to Story · refresh reads to view it");
        }
      } catch (value) {
        const insertError = asError(value);
        setError(insertError);
        setPhase(isConflict(insertError) ? "conflict" : "error");
      } finally {
        inFlightRef.current = false;
      }
    },
    [asset.acceptedFingerprint, contracts.editing.write, evidenceResult.evidence, target, usableTarget],
  );

  const refreshForReselect = useCallback(async () => {
    if (!target) return;
    setBlockedSelectionEpoch(attemptRef.current?.selectionEpoch ?? target.selectionEpoch);
    await target.onReload();
    attemptRef.current = undefined;
    setPhase("idle");
    setError(undefined);
    setNotice("Transcript selection preserved · reselect the Narrative insertion anchor before retrying");
  }, [target]);

  const retryIdentical = useCallback(() => {
    const attempt = attemptRef.current;
    if (attempt) void commit(attempt);
  }, [commit]);

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">TRANSCRIPT · EXACT TOKEN SELECTION</Text>
      {segments.map((segment, segmentIndex) => (
        <Stack key={segment.id} spacing="compact">
          <Text tone="eyebrow">
            {formatTime(segment.sourceRange.start)} → {formatTimeEnd(segment.sourceRange)}
          </Text>
          <TokenSelection
            disabled={phase === "saving" || phase === "conflict"}
            items={segment.tokens.map((token) => ({
              id: token.id,
              label: token.text.trim().length > 0 ? token.text : "space",
              selected: selectedTokenIds.has(token.id),
              text: token.text,
            }))}
            label={`Transcript segment ${segmentIndex + 1} tokens`}
            onSelect={(tokenId) => {
              setSelection((current) =>
                !current || current.artifactId !== page.artifact.id
                  ? { artifactId: page.artifact.id, anchorTokenId: tokenId, focusTokenId: tokenId }
                  : { ...current, focusTokenId: tokenId },
              );
              attemptRef.current = undefined;
              setPhase("idle");
              setError(undefined);
              setNotice(undefined);
            }}
          />
          <Button onPress={() => onContext(page, segment)}>Use this segment as @ context</Button>
        </Stack>
      ))}
      {segments.length === 0 ? <Text>No speech was recognized in this audio stream.</Text> : null}
      {evidenceResult.evidence ? <EvidenceSummary evidence={evidenceResult.evidence} target={usableTarget} /> : null}
      {evidenceResult.error ? <Status state="unavailable">{evidenceResult.error.message}</Status> : null}
      {selection ? (
        <Button
          disabled={phase === "saving" || phase === "conflict"}
          onPress={() => {
            setSelection(undefined);
            attemptRef.current = undefined;
            setPhase("idle");
            setError(undefined);
          }}
        >
          Clear excerpt selection
        </Button>
      ) : null}
      <Button
        disabled={
          phase === "saving" || !evidenceResult.evidence || !usableTarget || asset.acceptedFingerprint === undefined
        }
        onPress={() => void commit()}
      >
        {phase === "saving" ? "Inserting excerpt…" : "Insert excerpt"}
      </Button>
      {!asset.acceptedFingerprint ? <Status state="unavailable">Asset fingerprint is not accepted.</Status> : null}
      {phase === "error" && error ? (
        <>
          <Status state="unavailable">Insert failed · {error.message}</Status>
          {attemptRef.current ? <Button onPress={retryIdentical}>Retry identical excerpt insertion</Button> : null}
        </>
      ) : null}
      {phase === "conflict" && error ? (
        <>
          <Status state="unavailable">Insert conflict · token selection preserved</Status>
          <Button onPress={() => void refreshForReselect()}>Refresh and reselect Narrative target</Button>
        </>
      ) : null}
      {notice ? <Status state="ready">{notice}</Status> : null}
    </Stack>
  );
}

function EvidenceSummary({
  evidence,
  target,
}: Readonly<{ evidence: CreatorSourceExcerptEvidence; target?: CreatorExcerptTarget }>) {
  return (
    <Status state={target ? "ready" : "unavailable"}>
      {formatTime(evidence.sourceRange.start)} → {formatTimeEnd(evidence.sourceRange)} · {evidence.segmentIds.length}
      {" segments · "}
      {evidence.correctionRevisions.length} corrections · {target?.anchor.label ?? "select a Narrative target"}
    </Status>
  );
}

function sourceExcerptOperation(
  local: string,
  anchor: NarrativeInsertionAnchor,
  fingerprint: NonNullable<Asset["acceptedFingerprint"]>,
  evidence: CreatorSourceExcerptEvidence,
): CommitCreatorEditInput["operations"][number] {
  return {
    type: "insert-source-excerpt",
    createAs: local,
    parentId: anchor.parentId,
    ...(anchor.afterNodeId === undefined ? {} : { afterNodeId: anchor.afterNodeId }),
    assetId: evidence.assetId,
    acceptedFingerprint: fingerprint,
    transcriptArtifactId: evidence.artifactId,
    transcriptSegmentIds: evidence.segmentIds,
    sourceRange: evidence.sourceRange,
    language: evidence.language,
    correctionRevisions: evidence.correctionRevisions,
  };
}

function isConflict(value: Error): boolean {
  return value instanceof CreatorEditError && value.code === "conflict";
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
