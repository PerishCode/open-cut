package application

import (
	"bytes"
	"context"
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
		return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
	}
	canonicalParameters, parametersDigest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, claim.ParametersJSON) ||
		parametersDigest != claim.ParametersDigest {
		return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
	}
	expectedVideo, expectedAudio, err := SelectSourceProxyStreams(
		claim.SourceStreams, *parameters.RenderInputSelection,
	)
	if err != nil || !matchingRenderInputVideo(expectedVideo, execution.Video) ||
		!matchingRenderInputAudio(expectedAudio, execution.Audio) {
		return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
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
		return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
	}
	total := uint64(len(manifestCanonical)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		if manifest.Video.TimeMap.ByteSize.Value() > MaximumRenderInputArtifactSize-total {
			return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
		}
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	if total > MaximumRenderInputArtifactSize {
		return CompleteMediaRenderInput{}, domain.ErrInvalidMediaFacts
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
