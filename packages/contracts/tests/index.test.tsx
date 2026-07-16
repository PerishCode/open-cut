// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ContractsProvider,
  createContracts,
  digestString,
  durableID,
  int64String,
  revisionString,
  runtimePeer,
  useCreateProject,
  useProjects,
} from "../src/index.js";
import { emptyEventStream, jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  delete window.openCutPlatform;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("product runtime peer contracts", () => {
  it("keeps Web discovery identifiers in one pure public contract", () => {
    expect(runtimePeer.web).toEqual({ app: "web", httpEndpoint: "http" });
  });

  it("adapts exact generated Project reads and creator writes behind stable ports", async () => {
    let listCalls = 0;
    const fetchMock = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/projects" && init?.method !== "POST") {
        listCalls += 1;
        return jsonResponse(
          listSnapshot(listCalls === 1 ? [summary("alpha")] : [summary("alpha"), summary("beta")], listCalls),
        );
      }
      if (url === `/api/v1/projects/${ids.alpha}`) return jsonResponse(overview("alpha", "1"));
      if (url === "/api/v1/projects" && init?.method === "POST") {
        expect(init.body).toBe(JSON.stringify({ requestId: "gesture:create:1", name: "Beta" }));
        return jsonResponse(createdProject());
      }
      throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const contracts = createContracts();

    await expect(contracts.projects.read.list()).resolves.toMatchObject({ activityCursor: "1" });
    await expect(contracts.projects.read.show(durableID(ids.alpha))).resolves.toMatchObject({
      project: { id: ids.alpha, revision: "1" },
      format: { frameRate: { value: "30000", scale: 1001 } },
    });
    await expect(
      contracts.projects.write.create({ requestId: "gesture:create:1", name: "  Beta  " }),
    ).resolves.toMatchObject({
      project: { project: { id: ids.beta } },
      installationActivityCursor: "2",
    });
    expect(contracts.projects.read.getSnapshot()).toMatchObject({
      status: "ready",
      activityCursor: "2",
      projects: [{ id: ids.alpha }, { id: ids.beta }],
    });
  });

  it("rejects lossy exact scalars and non-normalized rational values", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          ...listSnapshot([summary("alpha")], 1),
          projects: [{ ...summary("alpha"), revision: 1 }],
        }),
      ),
    );
    await expect(createContracts().projects.read.list()).rejects.toThrow(/revision/);

    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({ ...overview("alpha", "1"), format: { ...format, frameRate: { value: "2", scale: 2 } } }),
      ),
    );
    await expect(createContracts().projects.read.show(durableID(ids.alpha))).rejects.toThrow(/not normalized/);
  });

  it("keeps trusted source selection and Asset registration behind one safe media port", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === `/_open-cut/platform/source-grants`) {
          const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
          expect(body).toEqual({ requestId: expect.stringMatching(/^ui:source-grant:/) });
          return jsonResponse(sourceGrantReceipt());
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets?limit=50`) {
          return jsonResponse({
            assets: [{ ...assetView(), availability: "online", facts: mediaFacts() }],
            activityCursor: "7",
          });
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets/${ids.asset}`) {
          return jsonResponse({
            asset: { ...assetView(), availability: "online", facts: mediaFacts() },
            activityCursor: "7",
          });
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets/${ids.asset}/media-leases` && init?.method === "POST") {
          expect(JSON.parse(String(init.body))).toEqual({ purpose: "source-preview", ...sourcePreviewSelection() });
          return jsonResponse({
            status: "ready",
            purpose: "source-preview",
            ...sourcePreviewPinFields(),
            diagnostics: [],
            job: {
              ...mediaJob(ids.proxyJob, "proxy", "succeeded"),
              progressBasisPoints: 10_000,
              resultArtifactId: ids.proxyArtifact,
            },
            lease: {
              schema: "open-cut/media-lease/v1",
              resourceId: ids.mediaLeaseResource,
              purpose: "source-preview",
              ...sourcePreviewIdentityFields(),
              artifactId: ids.proxyArtifact,
              artifactDigest: `sha256:${"a".repeat(64)}`,
              mimeType: "video/webm",
              byteLength: "4096",
              etag: `"sha256-${"b".repeat(64)}"`,
              sameOriginUrl: `/api/v1/media/content/oc_media_${"A".repeat(43)}`,
              expiresAt: "2026-07-15T09:05:00Z",
              sourceEpoch: { value: "0", scale: 1 },
              video: sourceTrackTiming(ids.sourceVideoStream, 1000),
              audio: sourceTrackTiming(ids.sourceAudioStream, 48000),
            },
          });
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets/${ids.asset}/source-position` && init?.method === "POST") {
          expect(JSON.parse(String(init.body))).toEqual({
            resourceId: ids.mediaLeaseResource,
            operation: "settle",
            target: { value: "1", scale: 2 },
          });
          return jsonResponse({
            resourceId: ids.mediaLeaseResource,
            ...sourcePreviewPinFields(),
            operation: "settle",
            requestedTime: { value: "1", scale: 2 },
            sourceTime: { value: "499", scale: 1000 },
            proxyTime: { value: "499", scale: 1000 },
            boundary: "video-presentation",
            atStart: false,
            atEnd: false,
          });
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets` && init?.method === "POST") {
          const body = JSON.parse(String(init.body)) as Record<string, unknown>;
          expect(body).toEqual({
            requestId: expect.stringMatching(/^ui:asset-register:/),
            sourceGrantId: ids.sourceGrant,
            importMode: "referenced",
            expectedProjectRevision: "1",
          });
          return jsonResponse({
            asset: {
              asset: { ...assetView(), sourceGrantId: ids.sourceGrant },
              availability: "identifying",
              artifacts: [],
              jobs: assetView().jobs,
            },
            transaction: { id: ids.mediaTransaction, committedProjectRevision: "2" },
            activityCursor: "8",
            replayed: false,
          });
        }
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );
    const media = createContracts().media;
    await expect(media.read.list(durableID(ids.alpha), { limit: 50 })).resolves.toMatchObject({
      assets: [
        {
          id: ids.asset,
          facts: {
            container: "matroska",
            bitRate: "1500000",
            streams: [
              {
                id: ids.sourceVideoStream,
                descriptor: { mediaType: "video", video: { width: 1920, height: 1080 } },
              },
              {
                id: ids.sourceAudioStream,
                descriptor: { mediaType: "audio", audio: { sampleRate: 48000 } },
              },
            ],
          },
          jobs: [{ kind: "identify" }, { kind: "probe" }],
        },
      ],
      activityCursor: "7",
    });
    const inspected = await media.read.inspect(durableID(ids.alpha), durableID(ids.asset));
    expect(inspected).toMatchObject({
      id: ids.asset,
      availability: "online",
    });
    if (!inspected.acceptedFingerprint) throw new Error("fixture Asset has no accepted fingerprint");
    const selection = {
      assetRevision: inspected.revision,
      fingerprint: inspected.acceptedFingerprint,
      videoStreamId: durableID(ids.sourceVideoStream),
      audioStreamId: durableID(ids.sourceAudioStream),
    };
    await expect(
      media.viewer.prepareSourcePreview(durableID(ids.alpha), durableID(ids.asset), selection),
    ).resolves.toMatchObject({
      status: "ready",
      job: { id: ids.proxyJob, kind: "proxy" },
      lease: {
        resourceId: ids.mediaLeaseResource,
        artifactId: ids.proxyArtifact,
        sameOriginUrl: expect.stringMatching(/^\/api\/v1\/media\/content\/oc_media_/),
      },
    });
    await expect(
      media.viewer.resolveSourcePosition(durableID(ids.alpha), durableID(ids.asset), selection, {
        resourceId: durableID(ids.mediaLeaseResource),
        operation: "settle",
        target: { value: int64String("1"), scale: 2 },
      }),
    ).resolves.toMatchObject({
      sourceTime: { value: "499", scale: 1000 },
      boundary: "video-presentation",
    });
    const imported = await media.write.importReferenced({
      projectId: durableID(ids.alpha),
      expectedProjectRevision: revisionString("1"),
    });
    expect(imported).toMatchObject({
      sourceGrant: { id: ids.sourceGrant, displayName: "clip.mov" },
      asset: { id: ids.asset },
      transactionId: ids.mediaTransaction,
      committedProjectRevision: "2",
      activityCursor: "8",
    });
    expect(imported?.asset).not.toHaveProperty("sourceGrantId");
    expect(fetch).not.toHaveBeenCalledWith("/api/v1/internal/platform/source-grants", expect.anything());
  });

  it("preserves typed terminal media failures", async () => {
    const failed = {
      ...mediaJob(ids.probeJob, "transcript", "failed"),
      terminalErrorCode: "transcript-output-invalid",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ assets: [{ ...assetView(), jobs: [failed] }], activityCursor: "8" })),
    );
    await expect(createContracts().media.read.list(durableID(ids.alpha), { limit: 50 })).resolves.toMatchObject({
      assets: [{ jobs: [{ state: "failed", terminalErrorCode: "transcript-output-invalid" }] }],
    });

    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          assets: [{ ...assetView(), jobs: [mediaJob(ids.probeJob, "transcript", "failed")] }],
          activityCursor: "8",
        }),
      ),
    );
    await expect(createContracts().media.read.list(durableID(ids.alpha), { limit: 50 })).rejects.toThrow(
      /media job is invalid/,
    );
  });

  it("treats closing the trusted source picker as a non-error cancellation", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 204 })),
    );
    await expect(
      createContracts().media.write.importReferenced({
        projectId: durableID(ids.alpha),
        expectedProjectRevision: revisionString("1"),
      }),
    ).resolves.toBeUndefined();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("seals a dropped File behind platform authority before source selection", async () => {
    const file = new File(["fixture"], "fixture.mov", { type: "video/quicktime" });
    const sourceToken = `drop.${"A".repeat(32)}`;
    const stageDroppedSource = vi.fn(async () => sourceToken);
    window.openCutPlatform = { stageDroppedSource };
    const requests: Array<{ url: string; body: string }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        requests.push({ url, body: String(init?.body ?? "") });
        if (url === `/_open-cut/platform/source-grants`) {
          expect(JSON.parse(String(init?.body))).toEqual({
            requestId: expect.stringMatching(/^ui:source-grant:/),
            sourceToken,
          });
          return jsonResponse(sourceGrantReceipt());
        }
        if (url === `/api/v1/projects/${ids.alpha}/assets`) {
          return jsonResponse({
            asset: {
              asset: { ...assetView(), sourceGrantId: ids.sourceGrant },
              availability: "identifying",
              artifacts: [],
              jobs: assetView().jobs,
            },
            transaction: { id: ids.mediaTransaction, committedProjectRevision: "2" },
            activityCursor: "8",
            replayed: false,
          });
        }
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );

    await expect(
      createContracts().media.write.importReferenced({
        projectId: durableID(ids.alpha),
        expectedProjectRevision: revisionString("1"),
        droppedFile: file,
      }),
    ).resolves.toMatchObject({ sourceGrant: { id: ids.sourceGrant }, asset: { id: ids.asset } });
    expect(stageDroppedSource).toHaveBeenCalledWith(file);
    expect(requests.every((request) => !request.body.includes("fixture.mov"))).toBe(true);
  });

  it("rejects dropped sources when the trusted platform bridge is unavailable or malformed", async () => {
    const file = new File(["fixture"], "fixture.mov");
    const input = {
      projectId: durableID(ids.alpha),
      expectedProjectRevision: revisionString("1"),
      droppedFile: file,
    };
    await expect(createContracts().media.write.importReferenced(input)).rejects.toThrow(/staging is unavailable/);

    window.openCutPlatform = { stageDroppedSource: async () => "/private/fixture.mov" };
    await expect(createContracts().media.write.importReferenced(input)).rejects.toThrow(/authority is invalid/);
  });

  it("keeps integrity preparation distinct from proxy job execution", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          status: "preparing",
          purpose: "source-preview",
          ...sourcePreviewPinFields(),
          stage: "integrity",
          diagnostics: [],
          job: {
            ...mediaJob(ids.proxyJob, "proxy", "succeeded"),
            progressBasisPoints: 10_000,
            resultArtifactId: ids.proxyArtifact,
          },
        }),
      ),
    );
    await expect(
      createContracts().media.viewer.prepareSourcePreview(
        durableID(ids.alpha),
        durableID(ids.asset),
        sourcePreviewSelection(),
      ),
    ).resolves.toEqual({
      status: "preparing",
      purpose: "source-preview",
      ...sourcePreviewPinFields(),
      stage: "integrity",
      diagnostics: [],
      job: expect.objectContaining({ id: ids.proxyJob, state: "succeeded" }),
    });
  });

  it("owns project-scoped media activity invalidation outside Web components", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === `/api/v1/projects/${ids.alpha}/assets?limit=50`) {
          return jsonResponse({ assets: [assetView()], activityCursor: "1" });
        }
        if (url === `/api/v1/events?projectId=${ids.alpha}&after=1`) {
          return mediaEventStream(init?.signal);
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const contracts = createContracts();
    let invalidations = 0;
    const unsubscribe = contracts.media.read.subscribe(durableID(ids.alpha), () => {
      invalidations += 1;
    });
    await contracts.media.read.list(durableID(ids.alpha), { limit: 50 });
    await waitFor(() => expect(invalidations).toBe(1));
    unsubscribe();
    contracts.close();
  });

  it("keeps product CLI pairing decisions behind creator authorization ports", async () => {
    const pairing = cliPairing("pending");
    const scopeUpgrade = cliScopeUpgrade("pending");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/authorization/cli/pairings" && init?.method !== "POST") {
          return jsonResponse({ grants: [pairing], upgrades: [scopeUpgrade] });
        }
        if (url.includes("/scope-upgrades/") && url.endsWith("/approve") && init?.method === "POST") {
          return jsonResponse({ upgrade: cliScopeUpgrade("approved"), grant: cliPairing("active", "2") });
        }
        if (url.includes("/scope-upgrades/") && url.endsWith("/deny") && init?.method === "POST") {
          return jsonResponse({ upgrade: cliScopeUpgrade("denied"), grant: cliPairing("active") });
        }
        if (url.endsWith("/approve") && init?.method === "POST") return jsonResponse(cliPairing("active"));
        if (url.endsWith("/deny") && init?.method === "POST") return jsonResponse(cliPairing("denied"));
        if (url.endsWith("/revoke") && init?.method === "POST") return jsonResponse(cliPairing("revoked"));
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );
    const authorization = createContracts().authorization;
    const listed = await authorization.readCLI();
    expect(listed.pairings).toMatchObject([
      { id: ids.pairing, status: "pending", revision: "1", scopes: ["activity:read", "project:read"] },
    ]);
    expect(listed.scopeUpgrades).toMatchObject([
      { id: ids.scopeUpgrade, grantId: ids.pairing, status: "pending", fromRevision: "1" },
    ]);
    await expect(authorization.approveCLIPairing(durableID(ids.pairing))).resolves.toMatchObject({ status: "active" });
    await expect(authorization.denyCLIPairing(durableID(ids.pairing))).resolves.toMatchObject({ status: "denied" });
    await expect(authorization.revokeCLIPairing(durableID(ids.pairing))).resolves.toMatchObject({ status: "revoked" });
    await expect(authorization.approveCLIScopeUpgrade(durableID(ids.scopeUpgrade))).resolves.toMatchObject({
      upgrade: { status: "approved" },
      grant: { revision: "2" },
    });
    await expect(authorization.denyCLIScopeUpgrade(durableID(ids.scopeUpgrade))).resolves.toMatchObject({
      upgrade: { status: "denied" },
    });
  });

  it("exposes creator writes through React hooks", async () => {
    let listCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/projects" && init?.method === "POST") return jsonResponse(createdProject());
        if (url === "/api/v1/projects") {
          listCalls += 1;
          return jsonResponse(
            listSnapshot(listCalls === 1 ? [summary("alpha")] : [summary("alpha"), summary("beta")], listCalls),
          );
        }
        if (url === "/api/v1/events?after=1") return emptyEventStream(init?.signal);
        throw new Error(["unexpected request", init?.method ?? "GET", url].join(" "));
      }),
    );
    const view = render(
      <ContractsProvider>
        <ProjectConsumer writable />
      </ContractsProvider>,
    );

    expect(await screen.findByText(`ready:1:${ids.alpha}`)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add Beta" }));
    expect(await screen.findByText(`ready:2:${ids.alpha},${ids.beta}`)).toBeTruthy();
    view.unmount();
  });

  it("refetches the snapshot after durable activity advances the cursor", async () => {
    let listCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/projects") {
          listCalls += 1;
          return jsonResponse(
            listSnapshot(listCalls === 1 ? [summary("alpha")] : [summary("alpha"), summary("beta")], listCalls),
          );
        }
        if (url === "/api/v1/events?after=1") return eventStream(init?.signal);
        if (url === "/api/v1/events?after=2") return emptyEventStream(init?.signal);
        throw new Error(["unexpected request", init?.method ?? "GET", url].join(" "));
      }),
    );
    const view = render(
      <ContractsProvider>
        <ProjectConsumer />
      </ContractsProvider>,
    );

    expect(await screen.findByText(`ready:2:${ids.alpha},${ids.beta}`)).toBeTruthy();
    expect(listCalls).toBe(2);
    view.unmount();
  });
});

function ProjectConsumer({ writable = false }: { writable?: boolean }) {
  const state = useProjects();
  const write = useCreateProject();
  return (
    <>
      <output>
        {[state.status, state.activityCursor, state.projects.map((project) => project.id).join(",")].join(":")}
      </output>
      {writable ? (
        <button type="button" onClick={() => void write.create({ requestId: "gesture:create:1", name: "Beta" })}>
          Add Beta
        </button>
      ) : null}
    </>
  );
}

const format = {
  canvasWidth: 1920,
  canvasHeight: 1080,
  pixelAspect: { value: "1", scale: 1 },
  frameRate: { value: "30000", scale: 1001 },
  audioSampleRate: 48000,
  audioLayout: "stereo",
  colorPolicy: "sdr-rec709",
} as const;

function summary(name: "alpha" | "beta") {
  const alpha = name === "alpha";
  return {
    id: alpha ? ids.alpha : ids.beta,
    revision: "1",
    lifecycleRevision: "1",
    name: alpha ? "Alpha" : "Beta",
    status: "active",
    narrativeDocumentId: alpha ? ids.alphaNarrative : ids.betaNarrative,
    mainSequenceId: alpha ? ids.alphaSequence : ids.betaSequence,
  };
}

function overview(name: "alpha" | "beta", activityCursor: string) {
  const alpha = name === "alpha";
  return {
    project: summary(name),
    narrativeDocumentRevision: "1",
    narrativeRootNodeId: alpha ? ids.alphaRoot : ids.betaRoot,
    mainSequenceRevision: "1",
    format,
    tracks: [
      { id: ids.video, revision: "1", type: "video", label: "V1" },
      { id: ids.audio, revision: "1", type: "audio", label: "A1" },
      { id: ids.caption, revision: "1", type: "caption", label: "C1" },
    ],
    activityCursor,
  };
}

function listSnapshot(projects: readonly ReturnType<typeof summary>[], cursor: number) {
  return { projects, activityCursor: String(cursor) };
}

function createdProject() {
  return {
    project: overview("beta", "1"),
    proposalId: ids.proposal,
    transactionId: ids.transaction,
    requestDigest: `sha256:${"a".repeat(64)}`,
    projectActivityCursor: "1",
    installationActivityCursor: "2",
    replayed: false,
  };
}

function sourceGrantReceipt() {
  return {
    grant: {
      id: ids.sourceGrant,
      platform: "mac",
      kind: "local-path-v1",
      displayName: "clip.mov",
      observation: { byteSize: "4096", modifiedUnixNs: "1784040000000000000", fileIdentity: "dev:1:ino:2" },
      state: "active",
      createdAt: "2026-07-15T09:00:00Z",
    },
    replayed: false,
  };
}

function assetView() {
  return {
    id: ids.asset,
    revision: "1",
    projectId: ids.alpha,
    displayName: "clip.mov",
    importMode: "referenced",
    acceptedFingerprint: `sha256:${"c".repeat(64)}`,
    tombstoned: false,
    availability: "identifying",
    artifacts: [],
    jobs: [mediaJob(ids.identifyJob, "identify", "queued"), mediaJob(ids.probeJob, "probe", "queued")],
  };
}

function sourcePreviewSelection() {
  return {
    assetRevision: revisionString("1"),
    fingerprint: digestString(`sha256:${"c".repeat(64)}`),
    videoStreamId: durableID(ids.sourceVideoStream),
    audioStreamId: durableID(ids.sourceAudioStream),
  };
}

function sourcePreviewPinFields() {
  return {
    ...sourcePreviewIdentityFields(),
    videoStreamId: ids.sourceVideoStream,
    audioStreamId: ids.sourceAudioStream,
  };
}

function sourcePreviewIdentityFields() {
  return {
    projectId: ids.alpha,
    assetId: ids.asset,
    assetRevision: "1",
    fingerprint: `sha256:${"c".repeat(64)}`,
  };
}

function sourceTrackTiming(sourceStreamId: string, scale: number) {
  return {
    sourceStreamId,
    coverageStart: { value: "0", scale: 1 },
    coverageDuration: { value: "1001", scale: 100 },
    sourceStartTime: { value: "0", scale: 1 },
    proxyStartTime: { value: "0", scale: 1 },
    sourceTimeBase: { value: "1", scale },
    proxyTimeBase: { value: "1", scale },
  };
}

function mediaFacts() {
  return {
    container: "matroska",
    containerAliases: ["webm"],
    startTime: { value: "0", scale: 1 },
    duration: { value: "1001", scale: 100 },
    bitRate: "1500000",
    streams: [
      {
        id: ids.sourceVideoStream,
        descriptor: {
          index: 0,
          mediaType: "video",
          codec: "vp9",
          codecProfile: "Profile 0",
          timeBase: { value: "1", scale: 1000 },
          duration: { value: "1001", scale: 100 },
          language: "und",
          dispositions: ["default"],
          video: {
            width: 1920,
            height: 1080,
            codedWidth: 1920,
            codedHeight: 1080,
            pixelAspect: { value: "1", scale: 1 },
            averageRate: { value: "30000", scale: 1001 },
            nominalRate: { value: "30000", scale: 1001 },
            rotation: 0,
            pixelFormat: "yuv420p",
            colorRange: "tv",
            colorSpace: "bt709",
            colorTransfer: "bt709",
            colorPrimaries: "bt709",
          },
        },
      },
      {
        id: ids.sourceAudioStream,
        descriptor: {
          index: 1,
          mediaType: "audio",
          codec: "opus",
          timeBase: { value: "1", scale: 48000 },
          duration: { value: "1001", scale: 100 },
          dispositions: ["default"],
          audio: { sampleFormat: "fltp", sampleRate: 48000, channels: 2, channelLayout: "stereo" },
        },
      },
    ],
  };
}

function mediaJob(id: string, kind: string, state: string) {
  return {
    id,
    kind,
    state,
    progressBasisPoints: 0,
    prerequisites: [],
    createdAt: "2026-07-15T09:00:00Z",
    updatedAt: "2026-07-15T09:00:00Z",
  };
}

function cliPairing(status: "pending" | "active" | "denied" | "revoked", revision = "1") {
  return {
    id: ids.pairing,
    installationId: "installation:test",
    agentId: ids.agent,
    publicKeyFingerprint: `sha256:${"b".repeat(64)}`,
    scopes: ["activity:read", "project:read"],
    revision,
    scopeDigest: `sha256:${"c".repeat(64)}`,
    status,
    createdAt: "2026-07-14T12:00:00Z",
    expiresAt: "2026-07-14T12:10:00Z",
    ...(status === "pending" ? {} : { decidedAt: "2026-07-14T12:01:00Z" }),
    ...(status === "revoked" ? { revokedAt: "2026-07-14T12:02:00Z" } : {}),
  };
}

function cliScopeUpgrade(status: "pending" | "approved" | "denied") {
  return {
    id: ids.scopeUpgrade,
    grantId: ids.pairing,
    fromRevision: "1",
    requestedScopes: ["activity:read", "project:read", "run:write"],
    requestedScopeDigest: `sha256:${"d".repeat(64)}`,
    status,
    createdAt: "2026-07-14T12:02:00Z",
    expiresAt: "2026-07-14T12:12:00Z",
    ...(status === "pending" ? {} : { decidedAt: "2026-07-14T12:03:00Z" }),
  };
}

function eventStream(signal?: AbortSignal | null): Response {
  const event = {
    schema: "open-cut/activity/v1",
    eventId: ids.event,
    scope: { kind: "installation", id: "installation:local" },
    cursor: "2",
    kind: "workspace.project-created",
    occurredAt: "2026-07-14T12:00:00Z",
    actor: { kind: "creator", id: ids.alpha },
    projectId: ids.beta,
    projectRevision: "1",
    changedEntityRefs: [{ kind: "project", id: ids.beta, revision: "1" }],
    outcome: { kind: "transaction", id: ids.transaction },
    summaryCode: "project.created",
  };
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(`id: 2\nevent: activity\ndata: ${JSON.stringify(event)}\n\n`));
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}

function mediaEventStream(signal?: AbortSignal | null): Response {
  const event = {
    schema: "open-cut/activity/v1",
    eventId: ids.event,
    scope: { kind: "project", id: ids.alpha },
    cursor: "2",
    kind: "media.identified",
    occurredAt: "2026-07-15T09:00:01Z",
    projectId: ids.alpha,
    projectRevision: "1",
    changedEntityRefs: [{ kind: "asset-media-state", id: ids.asset, revision: "1" }],
    outcome: { kind: "media-job", id: ids.identifyJob },
    summaryCode: "media-identified",
  };
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(`event: activity\ndata: ${JSON.stringify(event)}\n\n`));
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}
