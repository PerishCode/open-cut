package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	FullIdentityProfile       = "sha256-full-v1"
	MediaFactsProfile         = "ffprobe-facts-v1"
	SourceProxyProfile        = "webm-vp9-opus-source-v1"
	RenderInputProfile        = "matroska-ffv1-pcm-render-input-v1"
	WaveformProfile           = "waveform-rms-peak-v1"
	TranscriptProfile         = "whisper-small-multilingual-v1"
	SourceProxyArtifactSchema = "open-cut/source-proxy-artifact/v2"
	RenderInputArtifactSchema = "open-cut/render-input-artifact/v1"
)

type InitialMediaJobParameters struct {
	AssetID              domain.AssetID        `json:"assetId"`
	Kind                 domain.MediaJobKind   `json:"kind"`
	Profile              string                `json:"profile"`
	ProxySelection       *SourceProxySelection `json:"proxySelection,omitempty"`
	RenderInputSelection *SourceProxySelection `json:"renderInputSelection,omitempty"`
}

type SourceProxySelectionPolicy string

const (
	SourceProxySelectionDefault  SourceProxySelectionPolicy = "default-v1"
	SourceProxySelectionExplicit SourceProxySelectionPolicy = "explicit-v1"
)

type SourceProxySelection struct {
	Policy        SourceProxySelectionPolicy `json:"policy"`
	VideoStreamID *domain.SourceStreamID     `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID     `json:"audioStreamId,omitempty"`
}

func (selection SourceProxySelection) Validate() error {
	switch selection.Policy {
	case SourceProxySelectionDefault:
		if selection.VideoStreamID != nil || selection.AudioStreamID != nil {
			return domain.ErrInvalidMediaFacts
		}
	case SourceProxySelectionExplicit:
		if (selection.VideoStreamID == nil && selection.AudioStreamID == nil) ||
			(selection.VideoStreamID != nil && selection.VideoStreamID.IsZero()) ||
			(selection.AudioStreamID != nil && selection.AudioStreamID.IsZero()) {
			return domain.ErrInvalidMediaFacts
		}
	default:
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func (parameters InitialMediaJobParameters) Validate() error {
	profile, err := InitialMediaProfile(parameters.Kind)
	if parameters.AssetID.IsZero() || err != nil || parameters.Profile != profile {
		return domain.ErrInvalidMediaFacts
	}
	if parameters.Kind == domain.MediaJobProxy {
		if parameters.ProxySelection == nil || parameters.ProxySelection.Validate() != nil ||
			parameters.RenderInputSelection != nil {
			return domain.ErrInvalidMediaFacts
		}
	} else if parameters.Kind == domain.MediaJobRenderInput {
		if parameters.ProxySelection != nil || parameters.RenderInputSelection == nil ||
			parameters.RenderInputSelection.Validate() != nil ||
			parameters.RenderInputSelection.Policy != SourceProxySelectionExplicit ||
			(parameters.RenderInputSelection.VideoStreamID == nil) ==
				(parameters.RenderInputSelection.AudioStreamID == nil) {
			return domain.ErrInvalidMediaFacts
		}
	} else if parameters.ProxySelection != nil || parameters.RenderInputSelection != nil {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func InitialMediaProfile(kind domain.MediaJobKind) (string, error) {
	switch kind {
	case domain.MediaJobIdentify:
		return FullIdentityProfile, nil
	case domain.MediaJobProbe:
		return MediaFactsProfile, nil
	case domain.MediaJobProxy:
		return SourceProxyProfile, nil
	case domain.MediaJobRenderInput:
		return RenderInputProfile, nil
	case domain.MediaJobWaveform:
		return WaveformProfile, nil
	case domain.MediaJobTranscript:
		return TranscriptProfile, nil
	default:
		return "", fmt.Errorf("media job kind has no initial profile")
	}
}

func CanonicalInitialMediaJobParameters(
	parameters InitialMediaJobParameters,
) ([]byte, domain.Digest, error) {
	if err := parameters.Validate(); err != nil {
		return nil, "", err
	}
	return domain.CanonicalDigest(
		"open-cut/media-job-parameters", domain.MediaJobParametersSchema, parameters,
	)
}

func DecodeInitialMediaJobParameters(data []byte) (InitialMediaJobParameters, error) {
	var envelope struct {
		Domain  string                    `json:"domain"`
		Payload InitialMediaJobParameters `json:"payload"`
		Schema  string                    `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/media-job-parameters" ||
		envelope.Schema != domain.MediaJobParametersSchema || envelope.Payload.Validate() != nil {
		return InitialMediaJobParameters{}, domain.ErrInvalidMediaFacts
	}
	return envelope.Payload, nil
}
