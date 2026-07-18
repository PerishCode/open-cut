package application

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func (scheduler *mediaWorkDispatcher) materializeRenderInput(
	ctx context.Context,
	claim MediaJobClaim,
	execution MediaRenderInputExecution,
	eventID domain.ActivityEventID,
	at time.Time,
) (CompleteMediaRenderInput, error) {
	parameters, err := DecodeInitialMediaJobParameters(claim.ParametersJSON)
	if err != nil || parameters.AssetID != claim.AssetID || parameters.Kind != domain.MediaJobRenderInput ||
		parameters.Profile != RenderInputProfile || parameters.RenderInputSelection == nil ||
		claim.AcceptedFingerprint == nil || execution.Workspace == nil {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input job parameters are invalid")
	}
	canonicalParameters, parametersDigest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, claim.ParametersJSON) ||
		parametersDigest != claim.ParametersDigest {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input parameters are not canonical")
	}
	expectedVideo, expectedAudio, err := SelectSourceProxyStreams(
		claim.SourceStreams, *parameters.RenderInputSelection,
	)
	if err != nil {
		return CompleteMediaRenderInput{}, fmt.Errorf("select render-input source streams: %w", err)
	}
	if !matchingRenderInputVideo(expectedVideo, execution.Video) {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input video track does not match the selected source stream")
	}
	if !matchingRenderInputAudio(expectedAudio, execution.Audio) {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input audio track does not match the selected source stream")
	}
	artifactValue, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return CompleteMediaRenderInput{}, err
	}
	artifactID, err := domain.ParseArtifactID(artifactValue)
	if err != nil {
		return CompleteMediaRenderInput{}, err
	}
	manifest := RenderInputArtifactManifest{
		AssetID: claim.AssetID, Fingerprint: *claim.AcceptedFingerprint,
		Profile: parameters.Profile, Producer: claim.ExecutorVersion,
		SourceEpoch: execution.SourceEpoch, Media: execution.Media,
		Video: execution.Video, Audio: execution.Audio,
	}
	manifestCanonical, contentDigest, err := CanonicalRenderInputArtifactManifest(manifest)
	if err != nil || len(manifestCanonical) > MaximumRenderInputManifestSize {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input manifest is not canonical or exceeds its bound")
	}
	total := uint64(len(manifestCanonical)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		if manifest.Video.TimeMap.ByteSize.Value() > MaximumRenderInputArtifactSize-total {
			return CompleteMediaRenderInput{}, fmt.Errorf("render-input time map exceeds the artifact bound")
		}
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	if total > MaximumRenderInputArtifactSize {
		return CompleteMediaRenderInput{}, fmt.Errorf("render-input artifact total %d exceeds its bound", total)
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return CompleteMediaRenderInput{}, err
	}
	return CompleteMediaRenderInput{
		Claim: claim, ArtifactID: artifactID, Parameters: parameters, Manifest: manifest,
		ManifestCanonical: manifestCanonical, ContentDigest: contentDigest, ByteSize: byteSize,
		Workspace: execution.Workspace, EventID: eventID, CompletedAt: at,
	}, nil
}

func matchingRenderInputVideo(expected *domain.SourceStream, actual *RenderInputVideoTrack) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return reflect.DeepEqual(*expected, actual.Source)
}

func matchingRenderInputAudio(expected *domain.SourceStream, actual *RenderInputAudioTrack) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return reflect.DeepEqual(*expected, actual.Source)
}
