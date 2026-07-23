import { Button, EditorShell, EmptyState, Stack, Status, Tabs, Text } from "@open-cut/components";
import {
  type AgentContextAttachment,
  type Asset,
  type AssetPage,
  type CommandReceiptRef,
  type CreatorEditCommit,
  type DurableID,
  int64String,
  isProductFeatureAvailable,
  type NarrativeSubtree,
  type Project,
  type ProjectOverview,
  type RationalTime,
  type SequenceWindow,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { CreatorCaptionSource } from "../lib/creator-caption-controller.js";
import { createBackgroundWorkspaceInvalidation, workspaceStatus } from "../lib/creator-workspace-refresh.js";
import { SequenceViewerController } from "../lib/sequence-viewer-controller.js";
import { SourceViewerController } from "../lib/source-viewer-controller.js";
import { AgentAccess } from "./agent-access.js";
import {
  assetContext,
  captionContext,
  clipContext,
  creatorAgentContextCandidates,
  emptyWorkspaceSelection,
  includeWorkspaceSelection,
  narrativeContext,
  resolveWorkspaceFocus,
  sequencePointContext,
  sequenceRangeContext,
  transcriptSegmentContext,
  type WorkspaceSelection,
  workspaceFocusIntent,
} from "./creator-agent-context.js";
import { CreatorAgentPane } from "./creator-agent-pane.js";
import { CreatorCaptions } from "./creator-captions.js";
import { CreatorExport } from "./creator-export.js";
import { CreatorHistory } from "./creator-history.js";
import type { NarrativeInsertionAnchor } from "./creator-narrative-anchor.js";
import { CreatorNarrativeWriter } from "./creator-narrative-writer.js";
import {
  type CreatorRoughCutOccurrence,
  CreatorRoughCutPanel,
  createCreatorRoughCutOccurrence,
} from "./creator-rough-cut.js";
import { CreatorSourcePlacement } from "./creator-source-placement.js";
import { CreatorTimeline } from "./creator-timeline.js";
import { CreatorVersions } from "./creator-versions.js";
import { AssetSummary, type TranscriptState, TranscriptSurface } from "./creator-workspace-media.js";
import {
  mergeTranscriptCorrections,
  narrativeNodeID,
  type SourceStreamSelection,
  uniqueSourceStream,
  updateSourceStreamSelection,
} from "./creator-workspace-presentation.js";
import { SequencePreviewSurface, SourcePreviewSurface } from "./creator-workspace-viewer.js";
import { ManualCaptionEditor } from "./manual-caption-editor.js";
import { ProductAvailability, type ProductAvailabilityState } from "./product-availability.js";
import { ProductResources } from "./product-resources.js";
import { SourceImportSurface } from "./source-import-surface.js";

type WorkspaceState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "unavailable"; error: Error }>
  | Readonly<{
      status: "ready";
      overview: ProjectOverview;
      narrative: NarrativeSubtree;
      sequence: SequenceWindow;
      assets: AssetPage;
    }>;

type ReadyWorkspaceState = Extract<WorkspaceState, Readonly<{ status: "ready" }>>;

type RoughCutQueue = readonly CreatorRoughCutOccurrence[];

export function CreatorWorkspace({ project, onExit }: { project: Project; onExit?: () => void }) {
  const contracts = useContracts();
  const sequenceViewer = useMemo(() => new SequenceViewerController(contracts.media.viewer), [contracts]);
  const sourceViewer = useMemo(() => new SourceViewerController(contracts.media.viewer), [contracts]);
  const sequencePreview = useSyncExternalStore(
    sequenceViewer.subscribe,
    sequenceViewer.getSnapshot,
    sequenceViewer.getSnapshot,
  );
  const sourcePreview = useSyncExternalStore(
    sourceViewer.subscribe,
    sourceViewer.getSnapshot,
    sourceViewer.getSnapshot,
  );
  const [state, setState] = useState<WorkspaceState>({ status: "loading" });
  const [importing, setImporting] = useState(false);
  const [importError, setImportError] = useState<Error>();
  const [selectedAssetId, setSelectedAssetId] = useState<DurableID>();
  const [sourceStreamSelection, setSourceStreamSelection] = useState<SourceStreamSelection>();
  const [transcript, setTranscript] = useState<TranscriptState>({ status: "idle" });
  const [viewerMode, setViewerMode] = useState<"sequence" | "source">("sequence");
  const [workspaceSelection, setWorkspaceSelection] = useState<WorkspaceSelection>(emptyWorkspaceSelection);
  const [narrativeAnchor, setNarrativeAnchor] = useState<NarrativeInsertionAnchor>();
  const [narrativeSelectionEpoch, setNarrativeSelectionEpoch] = useState(0);
  const [roughCutOccurrences, setRoughCutOccurrences] = useState<RoughCutQueue>([]);
  const [roughCutTimelineStart, setRoughCutTimelineStart] = useState<RationalTime>();
  const [captionSource, setCaptionSource] = useState<CreatorCaptionSource>();
  const [historyRefreshEpoch, setHistoryRefreshEpoch] = useState(0);
  const [sourcePanel, setSourcePanel] = useState("source-media");
  const [productAvailability, setProductAvailability] = useState<ProductAvailabilityState>({ status: "loading" });

  const loadProductAvailability = useCallback(async () => {
    setProductAvailability({ status: "loading" });
    try {
      const snapshot = await contracts.product.read();
      setProductAvailability({ status: "ready", snapshot });
    } catch (value) {
      setProductAvailability({
        status: "unavailable",
        error: value instanceof Error ? value : new Error(String(value)),
      });
    }
  }, [contracts]);

  const load = useCallback(
    async (signal?: AbortSignal, preserveReady = false) => {
      if (!preserveReady) setState({ status: "loading" });
      try {
        const overview = await contracts.projects.read.show(project.id, signal);
        const [narrative, sequence, assets] = await Promise.all([
          contracts.editing.read.narrativeSubtree(
            {
              projectId: project.id,
              documentId: overview.project.narrativeDocumentId,
              parentId: overview.narrativeRootNodeId,
              limit: 200,
            },
            signal,
          ),
          contracts.editing.read.sequenceWindow(
            {
              projectId: project.id,
              sequenceId: overview.project.mainSequenceId,
              range: {
                start: { value: int64String("0"), scale: 1 },
                duration: { value: int64String("60"), scale: 1 },
              },
              limit: 512,
            },
            signal,
          ),
          contracts.media.read.list(project.id, { limit: 50 }, signal),
        ]);
        if (!signal?.aborted) {
          const readyState: ReadyWorkspaceState = { status: "ready", overview, narrative, sequence, assets };
          setState(readyState);
          return readyState;
        }
      } catch (value) {
        if (!signal?.aborted) {
          if (preserveReady) throw value;
          setState({ status: "unavailable", error: value instanceof Error ? value : new Error(String(value)) });
        }
      }
      return undefined;
    },
    [contracts, project.id],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const refreshCommittedWorkspace = useCallback(() => load(undefined, true), [load]);
  const reconcileBackgroundWorkspace = useMemo(() => createBackgroundWorkspaceInvalidation(load), [load]);
  const recordCreativeCommit = useCallback((_receipt: CreatorEditCommit) => {
    setHistoryRefreshEpoch((current) => current + 1);
  }, []);
  const recordAndRefreshCreativeCommit = useCallback(
    async (receipt: CreatorEditCommit) => {
      recordCreativeCommit(receipt);
      await refreshCommittedWorkspace();
    },
    [recordCreativeCommit, refreshCommittedWorkspace],
  );
  const refreshRestoredWorkspace = useCallback(async () => {
    setHistoryRefreshEpoch((current) => current + 1);
    setWorkspaceSelection(emptyWorkspaceSelection);
    setNarrativeAnchor(undefined);
    setRoughCutOccurrences([]);
    setRoughCutTimelineStart(undefined);
    setCaptionSource(undefined);
    const restored = await refreshCommittedWorkspace();
    if (restored) {
      sequenceViewer.setAvailableRevision(restored.sequence.sequenceRevision);
      sequenceViewer.adoptRevision(restored.sequence.sequenceRevision);
    }
  }, [refreshCommittedWorkspace, sequenceViewer]);

  useEffect(() => {
    void loadProductAvailability();
  }, [loadProductAvailability]);

  useEffect(
    () => contracts.media.read.subscribe(project.id, reconcileBackgroundWorkspace),
    [contracts, project.id, reconcileBackgroundWorkspace],
  );

  useEffect(
    () => () => {
      sequenceViewer.close();
      sourceViewer.close();
    },
    [sequenceViewer, sourceViewer],
  );

  const ready = state.status === "ready" ? state : undefined;
  const selectedAsset = ready?.assets.assets.find((asset) => asset.id === selectedAssetId);
  const sourceAsset = ready?.assets.assets.find((asset) => asset.id === sourceStreamSelection?.assetId);
  const sourceVideo = sourceAsset?.facts?.streams.find((stream) => stream.id === sourceStreamSelection?.videoStreamId)
    ?.descriptor.video;
  const sequencePreviewAvailable =
    productAvailability.status === "ready" &&
    isProductFeatureAvailable(productAvailability.snapshot, "sequence-preview");
  const sourcePreviewAvailable =
    productAvailability.status === "ready" && isProductFeatureAvailable(productAvailability.snapshot, "source-preview");
  const sequenceExportAvailable =
    productAvailability.status === "ready" &&
    isProductFeatureAvailable(productAvailability.snapshot, "sequence-export");
  const transcriptProjection =
    transcript.status === "resolved"
      ? { ...transcript.page, segments: transcript.segments, corrections: transcript.corrections }
      : undefined;
  const rootLastNode = ready?.narrative.nodes.at(-1);
  const activeNarrativeAnchor: NarrativeInsertionAnchor | undefined =
    narrativeAnchor ??
    (ready
      ? {
          parentId: ready.narrative.parent.id,
          parentRevision: ready.narrative.parent.revision,
          ...(rootLastNode ? { afterNodeId: narrativeNodeID(rootLastNode) } : {}),
          label: "root end",
        }
      : undefined);
  const selectionProjection = {
    assets: ready?.assets.assets ?? [],
    narrative: ready?.narrative,
    sequence: ready?.sequence,
    tracks: ready?.overview.tracks ?? [],
    transcript: transcriptProjection,
  };
  const selectContext = useCallback((attachment: AgentContextAttachment) => {
    setWorkspaceSelection((current) => includeWorkspaceSelection(current, attachment));
  }, []);
  const focusReceiptRef = useCallback(
    (ref: CommandReceiptRef): string => {
      const intent = workspaceFocusIntent(ref);
      if (!intent) return `Receipt reference ${ref.kind} ${ref.id} has no workspace focus adapter.`;
      const result = resolveWorkspaceFocus(intent, selectionProjection);
      if (result.assetId) {
        setSelectedAssetId(result.assetId);
      }
      if (result.sourceSurface) setSourcePanel(`source-${result.sourceSurface}`);
      const attachment = result.attachment;
      if (attachment) setWorkspaceSelection((current) => includeWorkspaceSelection(current, attachment));
      return result.notice;
    },
    [selectionProjection],
  );

  useEffect(() => {
    if (!ready) return;
    if (!sequencePreviewAvailable) {
      sequenceViewer.close();
      return;
    }
    sequenceViewer.open(project.id, ready.overview.project.mainSequenceId, ready.sequence.sequenceRevision);
  }, [project.id, ready, sequencePreviewAvailable, sequenceViewer]);

  useEffect(() => {
    const assets = ready?.assets.assets ?? [];
    setSelectedAssetId((current) =>
      current && assets.some((asset) => asset.id === current) ? current : assets[0]?.id,
    );
  }, [ready?.assets.assets]);

  const openSourceAsset = useCallback((asset: Asset) => {
    const streams = asset.facts?.streams ?? [];
    const video = uniqueSourceStream(streams, "video");
    const audio = uniqueSourceStream(streams, "audio");
    setSelectedAssetId(asset.id);
    setSourceStreamSelection({
      assetId: asset.id,
      ...(video ? { videoStreamId: video.id } : {}),
      ...(audio ? { audioStreamId: audio.id } : {}),
    });
    setViewerMode("source");
  }, []);

  useEffect(() => {
    if (!sourcePreviewAvailable) {
      sourceViewer.close();
      return;
    }
    if (
      !sourceAsset?.acceptedFingerprint ||
      (!sourceStreamSelection?.videoStreamId && !sourceStreamSelection?.audioStreamId)
    ) {
      sourceViewer.close();
      return;
    }
    sourceViewer.open({
      projectId: project.id,
      assetId: sourceAsset.id,
      assetRevision: sourceAsset.revision,
      fingerprint: sourceAsset.acceptedFingerprint,
      ...(sourceStreamSelection.videoStreamId === undefined
        ? {}
        : { videoStreamId: sourceStreamSelection.videoStreamId }),
      ...(sourceStreamSelection.audioStreamId === undefined
        ? {}
        : { audioStreamId: sourceStreamSelection.audioStreamId }),
    });
  }, [project.id, sourceAsset, sourcePreviewAvailable, sourceStreamSelection, sourceViewer]);

  const readTranscript = useCallback(
    async (assetId: DurableID, artifactId?: DurableID, signal?: AbortSignal) => {
      const currentDefault =
        transcript.status === "resolved" && transcript.assetId === assetId ? transcript.defaultArtifactId : undefined;
      setTranscript({ status: "loading", assetId });
      try {
        const page = await contracts.media.read.transcript(
          project.id,
          assetId,
          { ...(artifactId === undefined ? {} : { artifactId }), limit: 20 },
          signal,
        );
        const defaultArtifactId = page.artifact.isDefault ? page.artifact.id : currentDefault;
        if (!defaultArtifactId) throw new Error("Transcript default identity is unavailable");
        if (!signal?.aborted) {
          setTranscript({
            status: "resolved",
            assetId,
            page,
            segments: page.segments,
            corrections: page.corrections,
            defaultArtifactId,
            loadingMore: false,
            selectingDefault: false,
          });
        }
      } catch (value) {
        if (!signal?.aborted) {
          setTranscript({
            status: "unavailable",
            assetId,
            error: value instanceof Error ? value : new Error(String(value)),
          });
        }
      }
    },
    [contracts, project.id, transcript],
  );

  const readMoreTranscript = useCallback(async () => {
    if (transcript.status !== "resolved" || !transcript.page.nextAfter || transcript.loadingMore) return;
    const current = transcript;
    setTranscript({ ...current, loadingMore: true });
    try {
      const page = await contracts.media.read.transcript(project.id, current.assetId, {
        artifactId: current.page.artifact.id,
        after: current.page.nextAfter,
        limit: 20,
      });
      if (page.artifact.id !== current.page.artifact.id) throw new Error("Transcript continuation changed artifact");
      setTranscript({
        status: "resolved",
        assetId: current.assetId,
        page,
        segments: [...current.segments, ...page.segments],
        corrections: mergeTranscriptCorrections(current.corrections, page.corrections),
        defaultArtifactId: current.defaultArtifactId,
        loadingMore: false,
        selectingDefault: current.selectingDefault,
      });
    } catch (value) {
      setTranscript({
        status: "unavailable",
        assetId: current.assetId,
        error: value instanceof Error ? value : new Error(String(value)),
      });
    }
  }, [contracts, project.id, transcript]);

  const selectTranscriptDefault = useCallback(async () => {
    if (
      transcript.status !== "resolved" ||
      transcript.selectingDefault ||
      transcript.page.artifact.id === transcript.defaultArtifactId
    ) {
      return;
    }
    const current = transcript;
    setTranscript({ ...current, selectingDefault: true, selectionError: undefined });
    try {
      await contracts.media.write.selectTranscriptDefault(project.id, current.assetId, {
        artifactId: current.page.artifact.id,
        expectedDefaultArtifactId: current.defaultArtifactId,
      });
      setTranscript({
        ...current,
        page: { ...current.page, artifact: { ...current.page.artifact, isDefault: true } },
        defaultArtifactId: current.page.artifact.id,
        selectingDefault: false,
      });
    } catch (value) {
      setTranscript({
        ...current,
        selectingDefault: false,
        selectionError: value instanceof Error ? value : new Error(String(value)),
      });
    }
  }, [contracts, project.id, transcript]);

  const importFootage = useCallback(
    async (droppedFile?: File) => {
      if (state.status !== "ready" || importing) return;
      setImporting(true);
      setImportError(undefined);
      try {
        const imported = await contracts.media.write.importReferenced({
          projectId: project.id,
          expectedProjectRevision: state.overview.project.revision,
          ...(droppedFile ? { droppedFile } : {}),
        });
        if (imported) await load();
      } catch (value) {
        setImportError(value instanceof Error ? value : new Error(String(value)));
      } finally {
        setImporting(false);
      }
    },
    [contracts, importing, load, project.id, state],
  );

  const status = state.status === "loading" ? "pending" : state.status === "ready" ? "ready" : "unavailable";
  return (
    <EditorShell
      actions={
        <>
          {onExit ? <Button onPress={onExit}>Projects</Button> : null}
          <Button disabled={!ready || importing} onPress={() => void importFootage()}>
            {importing ? "Selecting…" : "Add footage"}
          </Button>
          <Button disabled={importing} onPress={() => void load()}>
            Refresh reads
          </Button>
        </>
      }
      brand="OPEN CUT"
      inspector={
        <CreatorAgentPane
          contextCandidates={creatorAgentContextCandidates(workspaceSelection, selectionProjection)}
          onAddPlayheadContext={
            ready ? () => selectContext(sequencePointContext(ready.sequence, sequencePreview.playhead)) : undefined
          }
          onAddTimelineContext={ready ? () => selectContext(sequenceRangeContext(ready.sequence)) : undefined}
          onFocusReceiptRef={focusReceiptRef}
          projectId={project.id}
          sequenceId={ready?.overview.project.mainSequenceId ?? project.mainSequenceId}
        />
      }
      inspectorLabel="Agent"
      sidebar={
        <Tabs
          activeTabId={sourcePanel}
          label="Creative sources"
          onTabChange={setSourcePanel}
          tabs={[
            {
              id: "source-media",
              label: "Media",
              content: (
                <Stack spacing="compact">
                  <SourceImportSurface
                    disabled={!ready || importing}
                    error={importError}
                    onSelect={(file) => void importFootage(file)}
                  />
                  {ready && ready.assets.assets.length === 0 ? (
                    <EmptyState hint="Add local footage to begin." title="No media yet" />
                  ) : null}
                  {ready?.assets.assets.map((asset) => (
                    <AssetSummary
                      asset={asset}
                      key={asset.id}
                      onContext={() => selectContext(assetContext(asset))}
                      onTranscript={() => {
                        setSelectedAssetId(asset.id);
                        setSourcePanel("source-transcript");
                        void readTranscript(asset.id);
                      }}
                      onPreview={() => openSourceAsset(asset)}
                      previewAvailable={sourcePreviewAvailable}
                      selected={viewerMode === "source" && asset.id === sourceStreamSelection?.assetId}
                    />
                  ))}
                </Stack>
              ),
            },
            {
              id: "source-story",
              label: "Story",
              content: (
                <Stack spacing="compact">
                  {ready ? (
                    <CreatorNarrativeWriter
                      narrative={ready.narrative}
                      onAddToRoughCut={(sourceExcerpt, evidenceStatus) => {
                        setRoughCutTimelineStart((current) => current ?? sequencePreview.playhead);
                        setRoughCutOccurrences((current) =>
                          current.length >= 128
                            ? current
                            : [
                                ...current,
                                createCreatorRoughCutOccurrence(
                                  sourceExcerpt,
                                  evidenceStatus,
                                  ready.assets.assets,
                                  ready.overview.tracks,
                                ),
                              ],
                        );
                      }}
                      onCreateCaptions={(sourceExcerpt, evidenceStatus) =>
                        setCaptionSource({ sourceExcerpt, evidenceStatus })
                      }
                      onReload={refreshCommittedWorkspace}
                      onCommitReceipt={recordCreativeCommit}
                      onSelect={(node, anchor) => {
                        setNarrativeAnchor(anchor);
                        setNarrativeSelectionEpoch((current) => current + 1);
                        selectContext(narrativeContext(node));
                      }}
                      projectId={project.id}
                      projectRevision={ready.overview.project.revision}
                      sequenceId={ready.overview.project.mainSequenceId}
                    />
                  ) : null}
                  {state.status === "loading" ? <Text>Loading story…</Text> : null}
                  {state.status === "unavailable" ? <Text>{state.error.message}</Text> : null}
                </Stack>
              ),
            },
            {
              id: "source-transcript",
              label: "Transcript",
              content: (
                <Stack spacing="compact">
                  {!selectedAsset ? (
                    <EmptyState hint="Choose media, then open its transcript." title="No source selected" />
                  ) : null}
                  <TranscriptSurface
                    asset={selectedAsset}
                    excerptTarget={
                      ready && activeNarrativeAnchor
                        ? {
                            projectId: project.id,
                            sequenceId: ready.overview.project.mainSequenceId,
                            projectRevision: ready.overview.project.revision,
                            anchor: activeNarrativeAnchor,
                            selectionEpoch: narrativeSelectionEpoch,
                            onCommitReceipt: recordCreativeCommit,
                            onReload: refreshCommittedWorkspace,
                            onInserted: (anchor) => {
                              setNarrativeAnchor(anchor);
                              setNarrativeSelectionEpoch((current) => current + 1);
                            },
                          }
                        : undefined
                    }
                    onContext={(page, segment) => selectContext(transcriptSegmentContext(page, segment))}
                    onInspect={(artifactId) => selectedAsset && void readTranscript(selectedAsset.id, artifactId)}
                    onLoad={selectedAsset ? () => void readTranscript(selectedAsset.id) : undefined}
                    onLoadMore={() => void readMoreTranscript()}
                    onSelectDefault={() => void selectTranscriptDefault()}
                    state={transcript}
                  />
                </Stack>
              ),
            },
          ]}
        />
      }
      sidebarLabel="Sources"
      status={<Status state={status}>{workspaceStatus(state.status)}</Status>}
      timeline={
        <Tabs
          label="Timeline panels"
          tabs={[
            {
              id: "timeline",
              label: "Timeline",
              content: (
                <Stack spacing="compact">
                  {ready ? (
                    <CreatorTimeline
                      assets={ready.assets.assets}
                      captions={ready.sequence.captions}
                      clips={ready.sequence.clips}
                      frameRate={ready.overview.format.frameRate}
                      onCommitted={recordAndRefreshCreativeCommit}
                      onContextClip={(clip) => selectContext(clipContext(clip))}
                      onReload={refreshCommittedWorkspace}
                      projectId={project.id}
                      range={ready.sequence.range}
                      sequenceId={ready.overview.project.mainSequenceId}
                      sequenceRevision={ready.sequence.sequenceRevision}
                      tracks={ready.overview.tracks}
                      viewer={sequenceViewer}
                    />
                  ) : null}
                </Stack>
              ),
            },
            {
              id: "rough-cut",
              label: "Rough cut",
              content: (
                <Stack spacing="compact">
                  {ready && roughCutOccurrences.length === 0 ? (
                    <EmptyState
                      hint="In Write, send a source excerpt to the rough cut — queued occurrences appear here for one atomic apply."
                      title="No rough cut queued"
                    />
                  ) : null}
                  {ready ? (
                    <CreatorRoughCutPanel
                      assets={ready.assets.assets}
                      currentPlayhead={sequencePreview.playhead}
                      occurrences={roughCutOccurrences}
                      onChange={setRoughCutOccurrences}
                      onCommitted={async (receipt) => {
                        setRoughCutOccurrences([]);
                        await recordAndRefreshCreativeCommit(receipt);
                      }}
                      onReload={refreshCommittedWorkspace}
                      onTimelineStartChange={setRoughCutTimelineStart}
                      projectId={project.id}
                      projectRevision={ready.overview.project.revision}
                      sequenceId={ready.overview.project.mainSequenceId}
                      sequenceRevision={ready.sequence.sequenceRevision}
                      timelineStart={roughCutTimelineStart ?? sequencePreview.playhead}
                      tracks={ready.overview.tracks}
                    />
                  ) : null}
                </Stack>
              ),
            },
            {
              id: "captions",
              label: "Captions",
              content: (
                <Stack spacing="compact">
                  {ready ? (
                    <CreatorCaptions
                      alignments={ready.sequence.alignments}
                      clips={ready.sequence.clips}
                      onCommitted={recordAndRefreshCreativeCommit}
                      onReload={refreshCommittedWorkspace}
                      projectId={project.id}
                      sequenceId={ready.overview.project.mainSequenceId}
                      source={captionSource}
                      tracks={ready.overview.tracks}
                    />
                  ) : null}
                  {ready ? (
                    <ManualCaptionEditor
                      captions={ready.sequence.captions}
                      onCommitted={recordAndRefreshCreativeCommit}
                      onContextCaption={(caption) => selectContext(captionContext(caption))}
                      onReload={refreshCommittedWorkspace}
                      projectId={project.id}
                      sequenceId={ready.overview.project.mainSequenceId}
                      tracks={ready.overview.tracks}
                      viewer={sequenceViewer}
                    />
                  ) : null}
                </Stack>
              ),
            },
            {
              id: "versions",
              label: "Versions",
              content: ready ? (
                <Stack spacing="compact">
                  <CreatorVersions
                    currentRevision={ready.overview.project.revision}
                    onRestored={refreshRestoredWorkspace}
                    projectId={project.id}
                    refreshEpoch={historyRefreshEpoch}
                  />
                  <CreatorHistory projectId={project.id} refreshEpoch={historyRefreshEpoch} />
                </Stack>
              ) : (
                <Text>Synchronizing project versions…</Text>
              ),
            },
            {
              id: "export",
              label: "Export",
              content: ready ? (
                <CreatorExport
                  available={sequenceExportAvailable}
                  hasContent={ready.sequence.clips.length > 0 || ready.sequence.captions.length > 0}
                  projectId={project.id}
                  projectName={project.name}
                  sequenceId={ready.overview.project.mainSequenceId}
                  sequenceRevision={ready.sequence.sequenceRevision}
                />
              ) : (
                <Text>Synchronizing the project…</Text>
              ),
            },
            {
              id: "system",
              label: "System",
              content: (
                <Stack spacing="compact">
                  <AgentAccess />
                  <ProductAvailability state={productAvailability} onRetry={() => void loadProductAvailability()} />
                  <ProductResources />
                </Stack>
              ),
            },
          ]}
        />
      }
      timelineLabel="Main sequence"
      title={project.name}
      viewer={
        <Stack spacing="compact">
          <Text tone="eyebrow">{viewerMode === "sequence" ? "SEQUENCE · VIEWER" : "SOURCE · VIEWER"}</Text>
          <Text>
            {viewerMode === "source"
              ? sourceVideo
                ? `${sourceVideo.width} × ${sourceVideo.height}`
                : sourceAsset?.facts
                  ? "Audio source"
                  : "Preparing source"
              : ready
                ? `${ready.overview.format.canvasWidth} × ${ready.overview.format.canvasHeight}`
                : "Preparing Sequence"}
          </Text>
          {viewerMode === "source" ? (
            <Button onPress={() => setViewerMode("sequence")}>Open Sequence Viewer</Button>
          ) : null}
          {viewerMode === "sequence" ? (
            sequencePreviewAvailable ? (
              <SequencePreviewSurface controller={sequenceViewer} snapshot={sequencePreview} />
            ) : (
              <Text>Sequence preview is unavailable in the active product build.</Text>
            )
          ) : (
            <SourcePreviewSurface
              asset={sourceAsset}
              audioStreamId={sourceStreamSelection?.audioStreamId}
              controller={sourceViewer}
              onAudioStreamChange={(streamId) =>
                setSourceStreamSelection((current) => updateSourceStreamSelection(current, "audio", streamId))
              }
              onVideoStreamChange={(streamId) =>
                setSourceStreamSelection((current) => updateSourceStreamSelection(current, "video", streamId))
              }
              snapshot={sourcePreview}
              videoStreamId={sourceStreamSelection?.videoStreamId}
            />
          )}
          {viewerMode === "source" && ready && sourceAsset ? (
            <CreatorSourcePlacement
              onCommitted={recordAndRefreshCreativeCommit}
              onShowSequence={() => setViewerMode("sequence")}
              sequenceId={ready.overview.project.mainSequenceId}
              sequenceSnapshot={sequencePreview}
              sequenceViewer={sequenceViewer}
              sourceSnapshot={sourcePreview}
              sourceViewer={sourceViewer}
              tracks={ready.overview.tracks}
            />
          ) : null}
        </Stack>
      }
      viewerLabel="Viewer"
    />
  );
}
