package application

import "github.com/PerishCode/open-cut/product/domain"

// RenderMaterial is the validated, profile-neutral vocabulary accepted by the
// shared RenderPlan compiler. Its fields stay private so callers can only
// construct it from a complete, canonical artifact manifest.
type RenderMaterial struct {
	kind           domain.ArtifactKind
	assetID        domain.AssetID
	fingerprint    domain.Digest
	producer       string
	profile        string
	sourceEpoch    domain.RationalTime
	mediaDigest    domain.Digest
	manifestDigest domain.Digest
	byteSize       domain.UInt64
	video          *domain.RenderVideoInput
	audio          *domain.RenderAudioInput
}

func NewSourceProxyRenderMaterial(manifest SourceProxyArtifactManifest) (RenderMaterial, error) {
	if manifest.Validate() != nil {
		return RenderMaterial{}, ErrRenderPlanInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/source-proxy-artifact", SourceProxyArtifactSchema, manifest,
	)
	if err != nil {
		return RenderMaterial{}, ErrRenderPlanInvalid
	}
	total := uint64(len(canonical)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return RenderMaterial{}, ErrRenderPlanInvalid
	}
	material := RenderMaterial{
		kind: domain.ArtifactProxy, assetID: manifest.AssetID, fingerprint: manifest.Fingerprint,
		producer: manifest.Producer, profile: manifest.Profile, sourceEpoch: manifest.SourceEpoch,
		mediaDigest: manifest.Media.SHA256, manifestDigest: digest, byteSize: byteSize,
	}
	if manifest.Video != nil {
		material.video = &domain.RenderVideoInput{
			SourceStreamID: manifest.Video.Source.ID, SourceStart: manifest.Video.SourceStartTime,
			MaterialStart:    manifest.Video.ProxyStartTime,
			SourceTimeBase:   manifest.Video.Source.Descriptor.TimeBase,
			MaterialTimeBase: manifest.Video.TimeBase, TimeMapDigest: manifest.Video.TimeMap.SHA256,
			Width: manifest.Video.Width, Height: manifest.Video.Height,
		}
	}
	if manifest.Audio != nil {
		material.audio = &domain.RenderAudioInput{
			SourceStreamID: manifest.Audio.Source.ID, SourceStart: manifest.Audio.SourceStartTime,
			MaterialStart:    manifest.Audio.ProxyStartTime,
			SourceTimeBase:   manifest.Audio.Source.Descriptor.TimeBase,
			MaterialTimeBase: manifest.Audio.TimeBase, SampleRate: manifest.Audio.SampleRate,
			ChannelLayout:      manifest.Audio.ChannelLayout,
			DecodedSampleCount: manifest.Audio.DecodedSampleCount,
		}
	}
	return material, nil
}

func NewRenderInputRenderMaterial(manifest RenderInputArtifactManifest) (RenderMaterial, error) {
	canonical, digest, err := CanonicalRenderInputArtifactManifest(manifest)
	if err != nil {
		return RenderMaterial{}, ErrRenderPlanInvalid
	}
	total := uint64(len(canonical)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil {
		return RenderMaterial{}, ErrRenderPlanInvalid
	}
	material := RenderMaterial{
		kind: domain.ArtifactRenderInput, assetID: manifest.AssetID, fingerprint: manifest.Fingerprint,
		producer: manifest.Producer, profile: manifest.Profile, sourceEpoch: manifest.SourceEpoch,
		mediaDigest: manifest.Media.SHA256, manifestDigest: digest, byteSize: byteSize,
	}
	if manifest.Video != nil {
		material.video = &domain.RenderVideoInput{
			SourceStreamID: manifest.Video.Source.ID, SourceStart: manifest.Video.SourceStartTime,
			MaterialStart:    manifest.Video.MaterialStartTime,
			SourceTimeBase:   manifest.Video.Source.Descriptor.TimeBase,
			MaterialTimeBase: manifest.Video.TimeBase, TimeMapDigest: manifest.Video.TimeMap.SHA256,
			Width: manifest.Video.Width, Height: manifest.Video.Height,
		}
	}
	if manifest.Audio != nil {
		material.audio = &domain.RenderAudioInput{
			SourceStreamID: manifest.Audio.Source.ID, SourceStart: manifest.Audio.SourceStartTime,
			MaterialStart:    manifest.Audio.MaterialStartTime,
			SourceTimeBase:   manifest.Audio.Source.Descriptor.TimeBase,
			MaterialTimeBase: manifest.Audio.TimeBase, SampleRate: manifest.Audio.SampleRate,
			ChannelLayout:      manifest.Audio.ChannelLayout,
			DecodedSampleCount: manifest.Audio.DecodedSampleCount,
		}
	}
	return material, nil
}

func (material RenderMaterial) ContainsStream(streamID domain.SourceStreamID) bool {
	return material.video != nil && material.video.SourceStreamID == streamID ||
		material.audio != nil && material.audio.SourceStreamID == streamID
}

func (material RenderMaterial) planInput(
	artifact domain.ArtifactSummary,
	asset RenderAssetSnapshot,
) (domain.RenderPlanInput, error) {
	if artifact.ID.IsZero() || artifact.Kind != material.kind || artifact.State != domain.ArtifactReady ||
		artifact.InputFingerprint != asset.AcceptedFingerprint || material.assetID != asset.ID ||
		material.fingerprint != asset.AcceptedFingerprint || material.producer != artifact.ProducerVersion ||
		material.manifestDigest != artifact.ContentDigest || material.byteSize != artifact.ByteSize {
		return domain.RenderPlanInput{}, ErrRenderPlanInvalid
	}
	input := domain.RenderPlanInput{
		ArtifactID: artifact.ID, ArtifactDigest: artifact.ContentDigest,
		ProducerVersion: artifact.ProducerVersion, Profile: material.profile,
		AssetID: asset.ID, AssetRevision: asset.Revision, Fingerprint: asset.AcceptedFingerprint,
		SourceEpoch: material.sourceEpoch, MediaDigest: material.mediaDigest,
	}
	if material.video != nil {
		video := *material.video
		input.Video = &video
	}
	if material.audio != nil {
		audio := *material.audio
		input.Audio = &audio
	}
	return input, nil
}
