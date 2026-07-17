// @vitest-environment jsdom

import { ContractsProvider } from "@open-cut/contracts";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HomeView } from "../../src/views/home-view.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("HomeView", () => {
  it("renders project state through Contracts hooks", async () => {
    const projectId = "018f0a60-7b80-7a01-8000-000000000001";
    const documentId = "018f0a60-7b80-7a01-8000-000000000002";
    const sequenceId = "018f0a60-7b80-7a01-8000-000000000003";
    const rootId = "018f0a60-7b80-7a01-8000-000000000004";
    const nodeId = "018f0a60-7b80-7a01-8000-000000000005";
    const assetId = "018f0a60-7b80-7a01-8000-000000000006";
    const proxyJobId = "018f0a60-7b80-7a01-8000-000000000007";
    const transcriptArtifactId = "018f0a60-7b80-7a01-8000-000000000008";
    const alternateTranscriptArtifactId = "018f0a60-7b80-7a01-8000-00000000000c";
    const transcriptStreamId = "018f0a60-7b80-7a01-8000-000000000009";
    const videoStreamId = "018f0a60-7b80-7a01-8000-00000000000d";
    const transcriptSegmentId = "018f0a60-7b80-7a01-8000-00000000000a";
    const transcriptTokenId = "018f0a60-7b80-7a01-8000-00000000000b";
    const transcriptCorrectionId = "018f0a60-7b80-7a01-8000-00000000000f";
    const excerptId = "018f0a60-7b80-7a01-8000-000000000010";
    let sequenceRequests = 0;
    let sourceRequests = 0;
    let exportRequests = 0;
    let exportHistoryRequests = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/projects") {
          return jsonResponse({
            activityCursor: "7",
            projects: [
              {
                id: projectId,
                revision: "1",
                lifecycleRevision: "1",
                name: "Alpha",
                status: "active",
                narrativeDocumentId: documentId,
                mainSequenceId: sequenceId,
              },
            ],
          });
        }
        if (url === "/api/v1/product/status") {
          return jsonResponse({
            schema: "open-cut/product-status/v1",
            features: [
              { feature: "asset-frame-inspection", state: "available" },
              { feature: "sequence-preview", state: "available" },
              { feature: "sequence-export", state: "available" },
              { feature: "source-preview", state: "available" },
              { feature: "local-transcription", state: "unavailable", reason: "not-qualified" },
            ],
          });
        }
        if (url === "/api/v1/product/resources") {
          return jsonResponse({ schema: "open-cut/product-resource-snapshot/v1", resources: [] });
        }
        if (url === `/api/v1/projects/${projectId}`) {
          return jsonResponse({
            project: {
              id: projectId,
              revision: "2",
              lifecycleRevision: "1",
              name: "Alpha",
              status: "active",
              narrativeDocumentId: documentId,
              mainSequenceId: sequenceId,
            },
            narrativeDocumentRevision: "2",
            narrativeRootNodeId: rootId,
            mainSequenceRevision: "2",
            format: {
              canvasWidth: 1920,
              canvasHeight: 1080,
              pixelAspect: { value: "1", scale: 1 },
              frameRate: { value: "30000", scale: 1001 },
              audioSampleRate: 48000,
              audioLayout: "stereo",
              colorPolicy: "sdr-rec709",
            },
            tracks: [],
            activityCursor: "7",
          });
        }
        if (url.includes(`/narratives/${documentId}/subtree`)) {
          return jsonResponse({
            documentId,
            documentRevision: "2",
            parent: { id: rootId, revision: "1", title: "Story", language: "en" },
            nodes: [
              {
                kind: "authored-text",
                authoredText: {
                  id: nodeId,
                  revision: "1",
                  documentId,
                  parentId: rootId,
                  purpose: "spoken",
                  language: "en",
                  text: "Open on a clear promise.",
                  tombstoned: false,
                },
              },
              {
                kind: "source-excerpt",
                evidenceStatus: "exact",
                sourceExcerpt: {
                  id: excerptId,
                  revision: "1",
                  documentId,
                  parentId: rootId,
                  afterNodeId: nodeId,
                  assetId,
                  acceptedFingerprint: `sha256:${"a".repeat(64)}`,
                  sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                  language: "en",
                  effectiveText: "A specific opening line.",
                  evidence: {
                    artifactId: transcriptArtifactId,
                    sourceStreamId: transcriptStreamId,
                    segmentIds: [transcriptSegmentId],
                    correctionRevisions: [{ id: transcriptCorrectionId, revision: "1" }],
                  },
                  tombstoned: false,
                },
              },
            ],
            activityCursor: "7",
          });
        }
        if (url.includes(`/sequences/${sequenceId}/window`)) {
          return jsonResponse({
            sequenceId,
            sequenceRevision: "2",
            range: { start: { value: "0", scale: 1 }, duration: { value: "60", scale: 1 } },
            clips: [],
            linkGroups: [],
            captions: [],
            alignments: [],
            activityCursor: "7",
          });
        }
        if (url === `/api/v1/projects/${projectId}/assets?limit=50`) {
          return jsonResponse({
            assets: [
              {
                id: assetId,
                revision: "1",
                projectId,
                displayName: "opening.mov",
                importMode: "referenced",
                acceptedFingerprint: `sha256:${"a".repeat(64)}`,
                tombstoned: false,
                availability: "online",
                fingerprint: `sha256:${"a".repeat(64)}`,
                facts: {
                  container: "matroska",
                  containerAliases: ["webm"],
                  duration: { value: "4", scale: 1 },
                  streams: [
                    {
                      id: videoStreamId,
                      descriptor: {
                        index: 0,
                        mediaType: "video",
                        codec: "vp9",
                        timeBase: { value: "1", scale: 1000 },
                        duration: { value: "4", scale: 1 },
                        dispositions: ["default"],
                        video: { width: 160, height: 90, rotation: 0 },
                      },
                    },
                    {
                      id: transcriptStreamId,
                      descriptor: {
                        index: 1,
                        mediaType: "audio",
                        codec: "opus",
                        timeBase: { value: "1", scale: 48000 },
                        duration: { value: "4", scale: 1 },
                        dispositions: ["default"],
                        audio: { sampleRate: 48000, channels: 1, channelLayout: "mono" },
                      },
                    },
                  ],
                },
                artifacts: [
                  {
                    id: transcriptArtifactId,
                    kind: "transcript",
                    producerVersion: `transcript@sha256:${"a".repeat(64)}`,
                    inputFingerprint: `sha256:${"b".repeat(64)}`,
                    state: "ready",
                    byteSize: "1024",
                    contentDigest: `sha256:${"c".repeat(64)}`,
                    createdAt: "2026-07-15T09:00:00Z",
                  },
                  {
                    id: alternateTranscriptArtifactId,
                    kind: "transcript",
                    producerVersion: `transcript@sha256:${"e".repeat(64)}`,
                    inputFingerprint: `sha256:${"b".repeat(64)}`,
                    state: "ready",
                    byteSize: "1024",
                    contentDigest: `sha256:${"f".repeat(64)}`,
                    createdAt: "2026-07-15T10:00:00Z",
                  },
                ],
                jobs: [],
              },
            ],
            activityCursor: "7",
          });
        }
        if (url === `/api/v1/projects/${projectId}/sequences/${sequenceId}/media-leases`) {
          sequenceRequests += 1;
          expect(JSON.parse(String(init?.body))).toEqual({
            purpose: "sequence-preview",
            operation: "prepare",
            expectedSequenceRevision: "2",
          });
          return jsonResponse({
            status: "empty",
            purpose: "sequence-preview",
            projectId,
            sequenceId,
            sequenceRevision: "2",
            diagnostics: [],
          });
        }
        if (url === `/api/v1/projects/${projectId}/assets/${assetId}/media-leases`) {
          sourceRequests += 1;
          expect(JSON.parse(String(init?.body))).toEqual({
            purpose: "source-preview",
            assetRevision: "1",
            fingerprint: `sha256:${"a".repeat(64)}`,
            videoStreamId,
            audioStreamId: transcriptStreamId,
          });
          return jsonResponse({
            status: "preparing",
            purpose: "source-preview",
            projectId,
            assetId,
            assetRevision: "1",
            fingerprint: `sha256:${"a".repeat(64)}`,
            videoStreamId,
            audioStreamId: transcriptStreamId,
            stage: "proxy",
            diagnostics: [],
            job: {
              id: proxyJobId,
              kind: "proxy",
              state: "queued",
              progressBasisPoints: 0,
              prerequisites: [],
              createdAt: "2026-07-15T09:00:00Z",
              updatedAt: "2026-07-15T09:00:00Z",
            },
          });
        }
        if (url === `/api/v1/projects/${projectId}/assets/${assetId}/transcript?limit=20`) {
          return jsonResponse({
            schema: "open-cut/transcript-read/v1",
            artifact: {
              id: transcriptArtifactId,
              assetId,
              sourceStreamId: transcriptStreamId,
              recognitionProfile: "whisper-small-multilingual-v1",
              engineVersion: `transcript@sha256:${"d".repeat(64)}`,
              engineTarget: "mac-arm64",
              modelName: "whisper-small-multilingual-v1",
              modelVersion: "whisper-small@c521a4b",
              detectedLanguage: "en",
              languageConfidenceBasisPoints: 9_700,
              sourceStartTime: { value: "0", scale: 1 },
              normalizedSampleCount: "32000",
              isDefault: true,
              createdAt: "2026-07-15T09:00:00Z",
            },
            segments: [
              {
                id: transcriptSegmentId,
                ordinal: 0,
                sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                text: "A precise opening line.",
                tokens: [
                  {
                    id: transcriptTokenId,
                    sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                    text: "A precise opening line.",
                    confidenceBasisPoints: 9_800,
                  },
                ],
              },
            ],
            corrections: [
              {
                id: transcriptCorrectionId,
                revision: "1",
                segmentIds: [transcriptSegmentId],
                sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                originalText: "A precise opening line.",
                effectiveText: "A specific opening line.",
                language: "en",
              },
            ],
            activityCursor: "7",
          });
        }
        if (
          url ===
          `/api/v1/projects/${projectId}/assets/${assetId}/transcript?artifactId=${alternateTranscriptArtifactId}&limit=20`
        ) {
          return jsonResponse({
            schema: "open-cut/transcript-read/v1",
            artifact: {
              id: alternateTranscriptArtifactId,
              assetId,
              sourceStreamId: transcriptStreamId,
              recognitionProfile: "whisper-small-multilingual-v1",
              engineVersion: `transcript@sha256:${"e".repeat(64)}`,
              engineTarget: "mac-arm64",
              modelName: "whisper-small-multilingual-v1",
              modelVersion: "whisper-small@c521a4b",
              detectedLanguage: "en",
              sourceStartTime: { value: "0", scale: 1 },
              normalizedSampleCount: "32000",
              isDefault: false,
              createdAt: "2026-07-15T10:00:00Z",
            },
            segments: [
              {
                id: "018f0a60-7b80-7a01-8000-00000000000d",
                ordinal: 0,
                sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                text: "An alternate recognition.",
                tokens: [
                  {
                    id: "018f0a60-7b80-7a01-8000-00000000000e",
                    sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
                    text: "An alternate recognition.",
                  },
                ],
              },
            ],
            corrections: [],
            activityCursor: "7",
          });
        }
        if (url === `/api/v1/projects/${projectId}/assets/${assetId}/transcript-selection`) {
          expect(init?.method).toBe("PUT");
          expect(JSON.parse(String(init?.body))).toEqual({
            artifactId: alternateTranscriptArtifactId,
            expectedDefaultArtifactId: transcriptArtifactId,
          });
          return jsonResponse({
            assetId,
            artifactId: alternateTranscriptArtifactId,
            previousArtifactId: transcriptArtifactId,
            selectedAt: "2026-07-15T10:01:00Z",
            activityCursor: "8",
            replayed: false,
          });
        }
        if (url === `/api/v1/projects/${projectId}/sequences/${sequenceId}/exports`) {
          exportRequests += 1;
          expect(init?.method).toBe("POST");
          expect(JSON.parse(String(init?.body))).toEqual(
            expect.objectContaining({ sequenceRevision: "2", preset: "webm-vp9-opus-v1" }),
          );
          return jsonResponse({
            projectId,
            sequenceId,
            sequenceRevision: "2",
            preset: "webm-vp9-opus-v1",
            job: {
              id: proxyJobId,
              rootJobId: proxyJobId,
              state: "blocked",
              progressBasisPoints: 0,
              createdAt: "2026-07-15T10:02:00Z",
              updatedAt: "2026-07-15T10:02:00Z",
            },
            recovery: "none",
            replayed: false,
            activityCursor: "9",
          });
        }
        if (url === `/api/v1/projects/${projectId}/exports?limit=20`) {
          exportHistoryRequests += 1;
          return jsonResponse({
            lineages:
              exportRequests === 0
                ? []
                : [
                    {
                      origin: "creator",
                      attemptCount: "1",
                      artifactAvailability: "none",
                      rootCreatedAt: "2026-07-15T10:02:00Z",
                      export: {
                        projectId,
                        sequenceId,
                        sequenceRevision: "2",
                        preset: "webm-vp9-opus-v1",
                        job: {
                          id: proxyJobId,
                          rootJobId: proxyJobId,
                          state: "blocked",
                          progressBasisPoints: 0,
                          createdAt: "2026-07-15T10:02:00Z",
                          updatedAt: "2026-07-15T10:02:00Z",
                        },
                        recovery: "none",
                        replayed: false,
                        activityCursor: "9",
                      },
                    },
                  ],
            activityCursor: exportRequests === 0 ? "7" : "9",
          });
        }
        if (url === "/api/v1/authorization/cli/pairings") {
          return jsonResponse(cliAuthorizationSnapshot());
        }
        if (url === "/api/v1/events?after=7") return eventStream(init?.signal);
        if (url === `/api/v1/projects/${projectId}/events?after=7`) return eventStream(init?.signal);
        throw new Error(["unexpected request", init?.method ?? "GET", url].join(" "));
      }),
    );
    const view = render(
      <ContractsProvider>
        <HomeView />
      </ContractsProvider>,
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Alpha" })).toBeTruthy();
    expect(await screen.findByText("Project r2 · Narrative r2 · Sequence r2")).toBeTruthy();
    expect(screen.getByText("Open on a clear promise.")).toBeTruthy();
    expect(screen.getByText("A specific opening line.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add footage" })).toBeTruthy();
    expect(await screen.findByText("The pinned Sequence is empty.")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "System" }));
    expect(await screen.findByText("Local transcription · not qualified for this build")).toBeTruthy();
    expect(await screen.findByText("No optional local resources are declared by this build.")).toBeTruthy();
    expect(screen.getByText("Main Sequence · pinned r2")).toBeTruthy();
    expect(sequenceRequests).toBe(1);
    expect(sourceRequests).toBe(0);
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    expect(await screen.findByText("EXPORT r2 · BLOCKED · 0% · CREATOR · 1 ATTEMPT")).toBeTruthy();
    expect(exportRequests).toBe(1);
    expect(exportHistoryRequests).toBeGreaterThanOrEqual(2);
    fireEvent.click(screen.getByRole("tab", { name: "Transcript" }));
    fireEvent.click(screen.getByRole("button", { name: "Open transcript" }));
    expect(await screen.findByText("A precise opening line.")).toBeTruthy();
    expect(screen.getByText("A precise opening line. → A specific opening line.")).toBeTruthy();
    expect(screen.getByText("en · whisper-small@c521a4b · DEFAULT")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Inspect transcript 1" }));
    expect(await screen.findByText("An alternate recognition.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Make this the Creator default" }));
    expect(await screen.findByText("en · whisper-small@c521a4b · DEFAULT")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open in Source Viewer" }));
    expect(await screen.findByText("SOURCE · VIEWER")).toBeTruthy();
    expect(sourceRequests).toBe(1);
    fireEvent.click(screen.getByRole("tab", { name: "Agent" }));
    expect(await screen.findByRole("button", { name: "Approve scope upgrade" })).toBeTruthy();
    expect(screen.getByText("Requested scopes: activity:read, project:read, run:write")).toBeTruthy();
    expect(screen.getByRole("main", { name: "Creator workspace" })).toBeTruthy();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    view.unmount();
  });
});

function cliAuthorizationSnapshot() {
  const pairingId = "018f0a60-7b80-7f01-8000-000000000001";
  return {
    grants: [
      {
        id: pairingId,
        installationId: "installation:test",
        agentId: "018f0a60-7b80-7f01-8000-000000000002",
        publicKeyFingerprint: `sha256:${"b".repeat(64)}`,
        scopes: ["activity:read", "project:read"],
        revision: "1",
        scopeDigest: `sha256:${"c".repeat(64)}`,
        status: "active",
        createdAt: "2026-07-14T12:00:00Z",
        expiresAt: "2026-07-14T12:10:00Z",
        decidedAt: "2026-07-14T12:01:00Z",
      },
    ],
    upgrades: [
      {
        id: "018f0a60-7b80-7f01-8000-000000000003",
        grantId: pairingId,
        fromRevision: "1",
        requestedScopes: ["activity:read", "project:read", "run:write"],
        requestedScopeDigest: `sha256:${"d".repeat(64)}`,
        status: "pending",
        createdAt: "2026-07-14T12:02:00Z",
        expiresAt: "2026-07-14T12:12:00Z",
      },
    ],
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function eventStream(signal?: AbortSignal | null): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}
