package main

// The media-tools build and check commands are a separate concern from serving:
// they run in a repository, produce and qualify artifact closures, and never
// touch a data directory or a session. Keeping them beside the server only made
// one file carry two lifecycles.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/internal/whispertoolchain"
	"github.com/PerishCode/open-cut/utils/target"
)

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
		// The whisper closure is built and qualified independently. It shares a
		// directory with the media closure because both ship beside the API
		// executable, and nothing else.
		whisperResult, err := whispertoolchain.Build(ctx, whispertoolchain.BuildOptions{
			RepositoryRoot: repositoryRoot, Target: target.Host(), Stdout: os.Stderr, Stderr: os.Stderr,
		})
		if err != nil {
			return fmt.Errorf("build API whisper artifact closure: %w", err)
		}
		state := "built"
		if whisperResult.Reused {
			state = "reused"
		}
		fmt.Fprintf(os.Stderr, "whisper toolchain %s (%s backend, %s)\n",
			whisperResult.Version, whisperResult.Backend, state)
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return err
		}
		if err := productresource.Write(
			filepath.Dir(executable), whisperResult.Version, productresource.DefaultResources(),
		); err != nil {
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
		whisperClosure, err := whispertoolchain.LoadForExecutable(executable, target.Host())
		if err != nil {
			return fmt.Errorf("verify API whisper artifact closure: %w", err)
		}
		if err := whispertoolchain.VerifyReleaseBaseline(whisperClosure); err != nil {
			return fmt.Errorf("verify API production transcription baseline: %w", err)
		}
		if err := whispertoolchain.VerifyCapabilities(ctx, whisperClosure); err != nil {
			return fmt.Errorf("verify API whisper capabilities: %w", err)
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
			"releaseProfile":        mediatoolchain.ReleaseBaselineProfile, "sourceSha256": sourceDigests,
			"transcriptionReleaseProfile": whispertoolchain.ReleaseBaselineProfile,
			"whisperVersion":              whisperClosure.Manifest.Version,
			"whisperBackend":              whisperClosure.Manifest.Build.Backend,
			"transcriberSha256":           whisperClosure.Capabilities[whispertoolchain.CapabilityLocalTranscriptionV1].Entry.SHA256,
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
