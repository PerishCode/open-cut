package application

import (
	"slices"

	"github.com/PerishCode/open-cut/product/domain"
)

const TranscriptProducerSchema = "open-cut/transcript-producer/v1"

func SelectDefaultTranscriptAudioStream(
	streams []domain.SourceStream,
) (domain.SourceStream, bool, error) {
	var selected *domain.SourceStream
	for _, stream := range streams {
		if stream.ID.IsZero() || stream.Descriptor.Validate() != nil {
			return domain.SourceStream{}, false, domain.ErrInvalidMediaFacts
		}
		if stream.Descriptor.MediaType != domain.MediaAudio {
			continue
		}
		candidate := stream
		if selected == nil || preferredTranscriptAudioStream(candidate, *selected) {
			selected = &candidate
		}
	}
	if selected == nil {
		return domain.SourceStream{}, false, nil
	}
	return *selected, true, nil
}

func CanonicalTranscriptBinding(
	binding domain.TranscriptBinding,
) ([]byte, domain.Digest, error) {
	if binding.Validate() != nil {
		return nil, "", domain.ErrInvalidTranscript
	}
	return domain.CanonicalDigest(
		"open-cut/transcript-binding", domain.TranscriptBindingSchema, binding,
	)
}

func TranscriptProducerVersion(binding domain.TranscriptBinding) (string, error) {
	if binding.Validate() != nil {
		return "", domain.ErrInvalidTranscript
	}
	_, digest, err := domain.CanonicalDigest(
		"open-cut/transcript-producer", TranscriptProducerSchema,
		struct {
			EngineVersion      string        `json:"engineVersion"`
			EngineTarget       string        `json:"engineTarget"`
			ModelEntryDigest   domain.Digest `json:"modelEntryDigest"`
			ModelContentDigest domain.Digest `json:"modelContentDigest"`
		}{
			EngineVersion: binding.EngineVersion, EngineTarget: binding.EngineTarget,
			ModelEntryDigest: binding.ModelEntryDigest, ModelContentDigest: binding.ModelContentDigest,
		},
	)
	if err != nil {
		return "", err
	}
	return "transcript@" + digest.String(), nil
}

func preferredTranscriptAudioStream(candidate, selected domain.SourceStream) bool {
	candidateDefault := slices.Contains(candidate.Descriptor.Dispositions, "default")
	selectedDefault := slices.Contains(selected.Descriptor.Dispositions, "default")
	if candidateDefault != selectedDefault {
		return candidateDefault
	}
	return candidate.Descriptor.Index < selected.Descriptor.Index
}
