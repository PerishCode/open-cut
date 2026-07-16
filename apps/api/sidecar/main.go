package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	sidecarclient "github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/target"
)

const httpEndpoint = "http"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "__media-executor" {
		return service.RunMediaExecutor(args[1:], os.Stdin, os.Stdout)
	}
	if len(args) == 2 && args[0] == "media-tools" {
		return runMediaTools(args[1])
	}
	if len(args) == 1 && args[0] == "openapi" {
		productStatus, err := service.NewProductStatusFromMediaTools(
			mediatoolchain.Verified{}, mediatoolchain.ErrUnavailable,
		)
		if err != nil {
			return err
		}
		projectStore := repository.NewMemoryProjects()
		projects, reads, activity, runs, edits, editReads, media, assetReads, err := productApplications(projectStore)
		if err != nil {
			return err
		}
		sourceAccess, err := service.NewSourceAccess(media, projectStore)
		if err != nil {
			return err
		}
		_, api := controller.NewRouter(
			service.NewHealth(repository.StaticHealth{}),
			productStatus,
			nil,
			projects, reads, activity, runs, edits, editReads, media, assetReads, sourceAccess,
			nil, nil, nil, nil, nil,
			service.RejectAuthorizer{},
		)
		document, err := api.OpenAPI().MarshalJSON()
		if err != nil {
			return fmt.Errorf("encode OpenAPI: %w", err)
		}
		_, err = os.Stdout.Write(append(document, '\n'))
		return err
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: api-sidecar [openapi|media-tools <build|check>]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	launch, err := protocol.LoadLaunchEnvironment()
	if err != nil {
		return err
	}
	dataDir, err := sidecarclient.ResolveDataDir(launch)
	if err != nil {
		return fmt.Errorf("resolve API data directory: %w", err)
	}
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		return err
	}
	defer projects.Close()
	if err := projects.ReconcileProductStorage(ctx, time.Now().UTC()); err != nil {
		return fmt.Errorf("reconcile API product storage: %w", err)
	}
	if err := projects.RecoverAgentBridgeTurns(ctx, time.Now().UTC()); err != nil {
		return fmt.Errorf("recover Agent bridge turns: %w", err)
	}
	projectsApplication, projectReads, activityReads, agentRuns, edits, editReads, media, assetReads, err := productApplications(projects)
	if err != nil {
		return err
	}
	sourceAccess, err := service.NewSourceAccess(media, projects)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve API executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve API executable identity: %w", err)
	}
	executor, err := service.NewExternalMediaIdentifyExecutor(
		sourceAccess, executable, filepath.Join(dataDir, "work", "media-attempts"), lifecycleProfile(launch.Mode),
	)
	if err != nil {
		return err
	}
	executors := []application.MediaJobExecutor{executor}
	var expectedSequenceRenderer *application.SequencePreviewRendererIdentity
	var expectedSequenceExportRenderer *application.RenderExecutorIdentity
	var expectedSequenceFont *domain.RenderFontResource
	var expectedSequenceExportFont *domain.RenderFontResource
	var sequenceFrameExecutorVersion string
	verified, mediaToolsErr := mediatoolchain.LoadForExecutable(executable, target.Host())
	resourceCatalog, resourceCatalogErr := productresource.LoadForExecutable(executable)
	productStatus, err := service.NewProductStatusFromClosures(
		verified, mediaToolsErr, resourceCatalog, resourceCatalogErr,
	)
	if err != nil {
		return err
	}
	if mediaToolsErr != nil {
		reason := application.ProductFeatureInvalid
		if errors.Is(mediaToolsErr, mediatoolchain.ErrUnavailable) {
			reason = application.ProductFeatureNotInstalled
		}
		fmt.Fprintf(os.Stderr, "media-derived product features unavailable: %s\n", reason)
	} else if _, exists := verified.Capabilities[mediatoolchain.CapabilitySequencePreviewRendererV1]; !exists {
		fmt.Fprintf(os.Stderr, "Sequence preview unavailable: %s\n", application.ProductFeatureNotQualified)
	}
	resourceEntries := resourceCatalog.Entries
	if resourceCatalogErr != nil {
		resourceEntries = nil
		reason := application.ProductFeatureInvalid
		if errors.Is(resourceCatalogErr, productresource.ErrUnavailable) {
			reason = application.ProductFeatureNotInstalled
		}
		fmt.Fprintf(os.Stderr, "local transcription resources unavailable: %s\n", reason)
	}
	productResources, err := application.NewProductResources(
		projects, resourceEntries, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return err
	}
	if mediaToolsErr == nil {
		tool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
		probe, probeErr := service.NewExternalMediaProbeExecutor(
			sourceAccess, tool.Path, verified.Manifest.Version+"@"+tool.SHA256,
			filepath.Join(dataDir, "work", "media-attempts"), lifecycleProfile(launch.Mode),
		)
		if probeErr != nil {
			return probeErr
		}
		frameTool := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1].Entry
		frameVersion := verified.Manifest.Version + "@" + tool.SHA256 + "@" + frameTool.SHA256 +
			"/" + application.FrameSetProfile
		frame, frameErr := service.NewExternalMediaFrameExecutor(
			sourceAccess, tool.Path, frameTool.Path, frameVersion,
			filepath.Join(dataDir, "work", "media-attempts"), lifecycleProfile(launch.Mode),
		)
		if frameErr != nil {
			return frameErr
		}
		proxyTool := verified.Capabilities[mediatoolchain.CapabilitySourceProxyV1].Entry
		proxyVersion := verified.Manifest.Version + "@" + tool.SHA256 + "@" + proxyTool.SHA256 +
			"/" + application.SourceProxyProfile
		proxy, proxyErr := service.NewExternalMediaProxyExecutor(
			sourceAccess, tool.Path, proxyTool.Path, proxyVersion,
			filepath.Join(dataDir, "work", "media-attempts"), lifecycleProfile(launch.Mode),
		)
		if proxyErr != nil {
			return proxyErr
		}
		renderInputCapability := verified.Capabilities[mediatoolchain.CapabilityRenderInputV1]
		renderInputVersion := verified.Manifest.Version + "/" + application.RenderInputProfile + "@" +
			renderInputCapability.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256
		renderInput, renderInputErr := service.NewExternalMediaRenderInputExecutor(
			sourceAccess, tool.Path, renderInputCapability.Entry.Path,
			renderInputVersion, verified.Manifest.Target.String(),
			filepath.Join(dataDir, "work", "media-render-input-attempts"), lifecycleProfile(launch.Mode),
		)
		if renderInputErr != nil {
			return renderInputErr
		}
		executors = append(executors, probe, frame, proxy, renderInput)
		frameCapability := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1]
		sequenceFrameExecutorVersion = verified.Manifest.Version + "/" +
			application.SequenceFrameSetProfile + "@" + frameCapability.ClosureSHA256 + "@" +
			verified.Manifest.Build.RecipeSHA256
		if transcriptCapability, exists := verified.Capabilities[mediatoolchain.CapabilityLocalTranscriptionV1]; exists && resourceCatalogErr == nil && transcriptCatalogCompatible(resourceCatalog) {
			models, modelErr := service.NewTranscriptResourceAccess(
				projects, dataDir, application.ClockFunc(time.Now),
			)
			if modelErr != nil {
				return modelErr
			}
			transcriptVersion := verified.Manifest.Version + "/" + application.TranscriptProfile + "@" +
				transcriptCapability.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256
			transcript, transcriptErr := service.NewExternalMediaTranscriptExecutor(
				sourceAccess, models, tool.Path, proxyTool.Path, transcriptCapability.Entry.Path,
				transcriptVersion, verified.Manifest.Target.String(),
				filepath.Join(dataDir, "work", "transcript-attempts"), lifecycleProfile(launch.Mode),
			)
			if transcriptErr != nil {
				return transcriptErr
			}
			executors = append(executors, transcript)
		}
		if identity, exists := sequencePreviewRendererIdentity(verified); exists {
			expectedSequenceRenderer = &identity
			capability := verified.Capabilities[mediatoolchain.CapabilitySequencePreviewRendererV1]
			if len(capability.Resources) != 1 {
				return fmt.Errorf("sequence preview renderer font closure is invalid")
			}
			resource := capability.Resources[0]
			font := domain.RenderFontResource{
				ResourceID: resource.ID, Version: resource.Version, SHA256: domain.Digest(resource.SHA256),
			}
			expectedSequenceFont = &font
		}
		if identity, exists := sequenceExportRendererIdentity(verified); exists {
			expectedSequenceExportRenderer = &identity
			capability := verified.Capabilities[mediatoolchain.CapabilitySequenceExportRendererV1]
			if len(capability.Resources) != 1 {
				return fmt.Errorf("sequence export renderer font closure is invalid")
			}
			resource := capability.Resources[0]
			font := domain.RenderFontResource{
				ResourceID: resource.ID, Version: resource.Version, SHA256: domain.Digest(resource.SHA256),
			}
			expectedSequenceExportFont = &font
		}
	}
	workExecutors, err := application.NewMediaWorkExecutors(
		projects, executors, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return err
	}
	resourceDownloader, err := service.NewProductResourceDownloader(
		nil, filepath.Join(dataDir, "work", "product-resource-downloads"),
	)
	if err != nil {
		return err
	}
	resourceExecutor, err := application.NewProductResourceWorkExecutor(
		projects, resourceDownloader, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return err
	}
	workExecutors = append(workExecutors, resourceExecutor)
	var sequencePreviews *application.SequencePreviews
	var sequenceFrames *application.SequenceFrames
	var sequenceExports *application.SequenceExports
	if expectedSequenceRenderer != nil {
		sequencePreviews, err = application.NewSequencePreviews(
			projects, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
			application.SequencePreviewSettings{
				RendererVersion: expectedSequenceRenderer.Version,
				RendererTarget:  expectedSequenceRenderer.Target,
				FontResource:    expectedSequenceFont,
			},
		)
		if err != nil {
			return err
		}
		sequenceFrames, err = application.NewSequenceFrames(
			projects, sequencePreviews, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
			application.SequenceFrameSettings{ExecutorVersion: sequenceFrameExecutorVersion},
		)
		if err != nil {
			return err
		}
	}
	exportSettings := application.SequenceExportSettings{}
	if expectedSequenceExportRenderer != nil {
		exportSettings = application.SequenceExportSettings{
			RendererVersion: expectedSequenceExportRenderer.Version,
			RendererTarget:  expectedSequenceExportRenderer.Target,
			FontResource:    expectedSequenceExportFont,
		}
	}
	sequenceExports, err = application.NewSequenceExports(
		projects, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now), exportSettings,
	)
	if err != nil {
		return err
	}
	if expectedSequenceRenderer != nil && mediaToolsErr == nil {
		if renderCapability, exists := verified.Capabilities[mediatoolchain.CapabilitySequencePreviewRendererV1]; exists {
			resourceRoots := make(map[string]string, len(renderCapability.Resources))
			for _, resource := range renderCapability.Resources {
				resourceRoots[resource.ID] = resource.Root
			}
			ffmpeg := verified.Tools["ffmpeg"]
			renderer, rendererErr := service.NewExternalSequencePreviewRenderer(
				projects, renderCapability.Entry.Path, *expectedSequenceRenderer,
				renderengine.ExecutionClosure{
					SHA256: domain.Digest(renderCapability.ClosureSHA256),
					Tools: map[string]renderengine.ExecutionToolPin{
						"ffmpeg": {Path: ffmpeg.Path, SHA256: domain.Digest(ffmpeg.SHA256)},
					},
				},
				resourceRoots,
				filepath.Join(dataDir, "work", "sequence-preview-attempts"), lifecycleProfile(launch.Mode),
			)
			if rendererErr != nil {
				return rendererErr
			}
			probeTool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
			verifier, verifierErr := service.NewExternalSequencePreviewVerifier(
				probeTool.Path, filepath.Join(dataDir, "work", "sequence-preview-verification"),
				lifecycleProfile(launch.Mode),
			)
			if verifierErr != nil {
				return verifierErr
			}
			previewExecutor, executorErr := application.NewSequencePreviewWorkExecutor(
				projects, renderer, verifier, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
			)
			if executorErr != nil {
				return executorErr
			}
			workExecutors = append(workExecutors, previewExecutor)
			frameCapability := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1]
			sequenceFrameExecutor, frameExecutorErr := service.NewExternalSequenceFrameExecutor(
				projects, frameCapability.Entry.Path, sequenceFrameExecutorVersion,
				filepath.Join(dataDir, "work", "sequence-frame-attempts"), lifecycleProfile(launch.Mode),
				application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
			)
			if frameExecutorErr != nil {
				return frameExecutorErr
			}
			workExecutors = append(workExecutors, sequenceFrameExecutor)
		}
	}
	if expectedSequenceExportRenderer != nil && mediaToolsErr == nil {
		if renderCapability, exists := verified.Capabilities[mediatoolchain.CapabilitySequenceExportRendererV1]; exists {
			resourceRoots := make(map[string]string, len(renderCapability.Resources))
			for _, resource := range renderCapability.Resources {
				resourceRoots[resource.ID] = resource.Root
			}
			ffmpeg := verified.Tools["ffmpeg"]
			renderer, rendererErr := service.NewExternalSequenceExportRenderer(
				projects, renderCapability.Entry.Path, *expectedSequenceExportRenderer,
				renderengine.ExecutionClosure{
					SHA256: domain.Digest(renderCapability.ClosureSHA256),
					Tools: map[string]renderengine.ExecutionToolPin{
						"ffmpeg": {Path: ffmpeg.Path, SHA256: domain.Digest(ffmpeg.SHA256)},
					},
				},
				resourceRoots,
				filepath.Join(dataDir, "work", "sequence-export-attempts"), lifecycleProfile(launch.Mode),
			)
			if rendererErr != nil {
				return rendererErr
			}
			probeTool := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
			verifier, verifierErr := service.NewExternalSequenceExportVerifier(
				probeTool.Path, filepath.Join(dataDir, "work", "sequence-export-verification"),
				lifecycleProfile(launch.Mode),
			)
			if verifierErr != nil {
				return verifierErr
			}
			exportExecutor, executorErr := application.NewSequenceExportWorkExecutor(
				projects, renderer, verifier, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
			)
			if executorErr != nil {
				return executorErr
			}
			workExecutors = append(workExecutors, exportExecutor)
		}
	}
	scheduler, err := application.NewWorkScheduler(
		projects, workExecutors,
		application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
		application.WorkSchedulerSettings{
			LeaseOwner:    fmt.Sprintf("api:%d", os.Getpid()),
			LeaseDuration: 30 * time.Second, PollInterval: 200 * time.Millisecond,
			Resources: productResources.RuntimeRegistrations(),
		},
	)
	if err != nil {
		return err
	}
	if err := scheduler.Recover(ctx); err != nil {
		return fmt.Errorf("recover work scheduler: %w", err)
	}
	schedulerContext, stopScheduler := context.WithCancel(ctx)
	defer stopScheduler()
	schedulerStopped := make(chan error, 1)
	go func() { schedulerStopped <- scheduler.Run(schedulerContext) }()
	authorizer, err := localAuthorizer(ctx, launch, projects)
	if err != nil {
		return err
	}
	agentBridges, err := application.NewAgentBridges(
		projects, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return err
	}
	agentBridge, adapterState, err := localAgentBridge(
		ctx, dataDir, lifecycleProfile(launch.Mode), agentBridges, projects,
	)
	if err != nil {
		return err
	}
	if adapterState != "ready" {
		fmt.Fprintf(os.Stderr, "local Agent adapter unavailable: %s\n", adapterState)
	}
	viewerMedia, err := application.NewViewerMedia(
		projects, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return err
	}
	mediaLeases, err := service.NewMediaLeaseService(
		viewerMedia, projects, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now), rand.Reader,
	)
	if err != nil {
		return err
	}
	var sequencePreviewLeases *service.SequencePreviewLeaseService
	if sequencePreviews != nil {
		var previewErr error
		sequencePreviewLeases, previewErr = service.NewSequencePreviewLeaseService(
			sequencePreviews, projects, application.UUIDv7IdentityGenerator{},
			application.ClockFunc(time.Now), rand.Reader,
		)
		if previewErr != nil {
			return previewErr
		}
	}
	sequenceExportDelivery, err := service.NewSequenceExportDeliveryService(
		projects, application.ClockFunc(time.Now), rand.Reader,
	)
	if err != nil {
		return err
	}
	mux, _ := controller.NewRouterWithAgentBridge(
		service.NewHealth(repository.StaticHealth{}),
		productStatus,
		productResources,
		projectsApplication, projectReads, activityReads, agentRuns, edits, editReads,
		media, assetReads, sourceAccess,
		mediaLeases,
		sequencePreviewLeases,
		sequenceFrames,
		sequenceExports,
		sequenceExportDelivery,
		agentBridge,
		authorizer,
	)
	session, err := sidecarclient.DialSession(ctx, launch.Control, launch.Token, sidecarclient.Registration{
		Channel: launch.Channel, Namespace: launch.Namespace, App: launch.App,
		Mode: launch.Mode, Source: launch.Source,
	})
	if err != nil {
		return fmt.Errorf("connect API sidecar: %w", err)
	}
	defer session.Close(0)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for API: %w", err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	endpoint := "http://" + listener.Addr().String()
	if err := session.Endpoint(httpEndpoint, endpoint); err != nil {
		return shutdownServer(server, fmt.Errorf("publish API endpoint: %w", err))
	}
	if err := session.Ready(); err != nil {
		return shutdownServer(server, fmt.Errorf("publish API readiness: %w", err))
	}

	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return shutdownServer(server, nil)
		case err := <-served:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("serve API: %w", err)
		case err := <-schedulerStopped:
			if err == nil && ctx.Err() != nil {
				return shutdownServer(server, nil)
			}
			if err == nil {
				return shutdownServer(server, fmt.Errorf("work scheduler stopped unexpectedly"))
			}
			return shutdownServer(server, fmt.Errorf("work scheduler stopped: %w", err))
		case <-heartbeat.C:
			if err := projects.ReconcileMediaScratchLeases(ctx, time.Now().UTC()); err != nil {
				return shutdownServer(server, fmt.Errorf("reconcile media scratch leases: %w", err))
			}
			_ = session.Heartbeat()
		default:
			commandContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			command, readErr := session.ReadCommand(commandContext)
			cancel()
			if readErr == nil && command == protocol.ControlCommandShutdown {
				return shutdownServer(server, nil)
			}
		}
	}
}

func runMediaTools(mode string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch mode {
	case "build":
		repositoryRoot, err := sourceRepositoryRoot()
		if err != nil {
			return err
		}
		result, err := mediatoolchain.Build(ctx, mediatoolchain.BuildOptions{
			RepositoryRoot: repositoryRoot, Target: target.Host(), Stdout: os.Stderr, Stderr: os.Stderr,
		})
		if err != nil {
			return fmt.Errorf("build API media artifact closure: %w", err)
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return err
		}
		if err := productresource.Write(filepath.Dir(executable), result.Version, productresource.DefaultResources()); err != nil {
			return fmt.Errorf("build product resource catalog: %w", err)
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "check":
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve API executable: %w", err)
		}
		verified, err := mediatoolchain.LoadForExecutable(executable, target.Host())
		if err != nil {
			return fmt.Errorf("verify API media artifact closure: %w", err)
		}
		if err := mediatoolchain.VerifyReleaseBaseline(verified); err != nil {
			return fmt.Errorf("verify API production media baseline: %w", err)
		}
		if err := mediatoolchain.VerifyCapabilities(ctx, verified); err != nil {
			return fmt.Errorf("verify API media capabilities: %w", err)
		}
		if _, err := productresource.LoadForExecutable(executable); err != nil {
			return fmt.Errorf("verify product resource catalog: %w", err)
		}
		probe := verified.Capabilities[mediatoolchain.CapabilityProbeV1].Entry
		frameDecoder := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1].Entry
		proxyEncoder := verified.Capabilities[mediatoolchain.CapabilitySourceProxyV1].Entry
		renderInput := verified.Capabilities[mediatoolchain.CapabilityRenderInputV1].Entry
		previewRenderer := verified.Capabilities[mediatoolchain.CapabilitySequencePreviewRendererV1].Entry
		exportRenderer := verified.Capabilities[mediatoolchain.CapabilitySequenceExportRendererV1].Entry
		transcriber := verified.Capabilities[mediatoolchain.CapabilityLocalTranscriptionV1].Entry
		sourceDigests := make([]string, len(verified.Manifest.Sources))
		for index, source := range verified.Manifest.Sources {
			sourceDigests[index] = source.ID + "@" + source.SHA256
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema": 1, "target": verified.Manifest.Target.String(), "version": verified.Manifest.Version,
			"probeSha256": probe.SHA256, "frameSha256": frameDecoder.SHA256,
			"proxySha256": proxyEncoder.SHA256, "renderInputSha256": renderInput.SHA256,
			"previewRendererSha256": previewRenderer.SHA256,
			"exportRendererSha256":  exportRenderer.SHA256,
			"transcriberSha256":     transcriber.SHA256,
			"releaseProfile":        mediatoolchain.ReleaseBaselineProfile, "sourceSha256": sourceDigests,
		})
	default:
		return fmt.Errorf("usage: api-sidecar media-tools <build|check>")
	}
}

func sourceRepositoryRoot() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	workingDirectory, err = filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	for candidate := workingDirectory; ; candidate = filepath.Dir(candidate) {
		if repositoryMarkers(candidate) && containedPath(candidate, executable) {
			return candidate, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return "", fmt.Errorf("API media build requires its source repository working tree")
}

func repositoryMarkers(root string) bool {
	for _, name := range []string{"go.mod", "oc-control.json", "pnpm-workspace.yaml"} {
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func containedPath(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func lifecycleProfile(mode protocol.LifecycleMode) lifecycle.Profile {
	switch mode {
	case protocol.LifecycleModeProduction:
		return lifecycle.ProfileProduction
	case protocol.LifecycleModePackaged:
		return lifecycle.ProfilePackaged
	case protocol.LifecycleModeHarness:
		return lifecycle.ProfileHarness
	default:
		return lifecycle.ProfileDevelopment
	}
}

func localAuthorizer(
	ctx context.Context,
	launch protocol.SidecarLaunch,
	store localAuthorizationStore,
) (service.CombinedAuthorizer, error) {
	encoded := make(map[string]string)
	for _, key := range launch.Installation.Keys {
		if key.Algorithm == protocol.InstallationKeyAlgorithmEd25519 {
			encoded[key.Role] = key.PublicKey
		}
	}
	decodeRole := func(role string) (ed25519.PublicKey, error) {
		publicKey, err := base64.StdEncoding.DecodeString(encoded[role])
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("installation has no valid %s public key", role)
		}
		return ed25519.PublicKey(publicKey), nil
	}
	uiPublicKey, err := decodeRole(service.UIRole)
	if err != nil {
		return service.CombinedAuthorizer{}, err
	}
	cliPublicKey, err := decodeRole(service.CLIRole)
	if err != nil {
		return service.CombinedAuthorizer{}, err
	}
	allowDevelopment := launch.Mode == protocol.LifecycleModeDev
	identities := application.UUIDv7IdentityGenerator{}
	clock := application.ClockFunc(time.Now)
	ui, err := service.NewUISessionService(ctx, service.UISessionConfig{
		InstallationID:         launch.Installation.InstallationID,
		InstallationGeneration: launch.Installation.Generation,
		CellGeneration:         launch.Control.Generation,
		PublicKey:              uiPublicKey, AllowedOrigins: []string{"oc://app"},
		AllowDevelopmentOrigin: allowDevelopment,
	}, store, identities, clock, rand.Reader)
	if err != nil {
		return service.CombinedAuthorizer{}, err
	}
	cli, err := service.NewCLIAuthorizationService(ctx, service.CLIChallengeConfig{
		InstallationID: launch.Installation.InstallationID, InstallationGeneration: launch.Installation.Generation,
		CellGeneration: launch.Control.Generation, PublicKey: cliPublicKey,
	}, store, store, identities, clock, rand.Reader)
	if err != nil {
		return service.CombinedAuthorizer{}, err
	}
	return service.CombinedAuthorizer{UI: ui, CLI: cli}, nil
}

type localAuthorizationStore interface {
	application.AuthorizationRepository
	application.AgentRunBindingRepository
}

type projectStore interface {
	application.ProjectRepository
	application.ProjectReadRepository
	application.ActivityRepository
	application.AgentRunRepository
	application.EditRepository
	application.EditReadRepository
	application.MediaRepository
	application.AssetReadRepository
	application.MediaWorkRepository
	ReadAssetSourceMaterial(context.Context, domain.AssetID) (domain.SourceGrantSummary, []byte, error)
}

func productApplications(
	store projectStore,
) (
	*application.Projects,
	*application.ProjectReads,
	*application.ActivityReads,
	*application.AgentRuns,
	*application.Edits,
	*application.EditReads,
	*application.Media,
	*application.AssetReads,
	error,
) {
	projects, err := application.NewProjects(
		store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now),
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	reads, err := application.NewProjectReads(store)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	activity, err := application.NewActivityReads(store)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	runs, err := application.NewAgentRuns(store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	editReads, err := application.NewEditReads(store)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, application.ClockFunc(time.Now))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	assetReads, err := application.NewAssetReads(store)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	return projects, reads, activity, runs, edits, editReads, media, assetReads, nil
}

func shutdownServer(server *http.Server, result error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); result == nil && err != nil {
		return err
	}
	return result
}
