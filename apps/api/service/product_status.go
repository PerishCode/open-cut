package service

import (
	"errors"

	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/internal/whispertoolchain"
	"github.com/PerishCode/open-cut/product/application"
)

func NewProductStatusFromMediaTools(
	verified mediatoolchain.Verified,
	loadErr error,
) (*application.ProductStatus, error) {
	return NewProductStatusFromClosures(
		verified, loadErr,
		whispertoolchain.Verified{}, whispertoolchain.ErrUnavailable,
		productresource.Verified{}, productresource.ErrUnavailable,
	)
}

// NewProductStatusFromClosures projects one feature row per capability from the
// closures that own them.
//
// Transcription reads three sources, and the media closure is deliberately
// still one of them. Splitting whisper out separated the two closures'
// identities and builds, not the runtime pipeline: the API still normalizes
// arbitrary source audio to canonical 16 kHz mono S16 with the media closure's
// FFmpeg before whisper is invoked. Reporting transcription available while the
// normalizer is missing would promise a feature whose jobs cannot run.
func NewProductStatusFromClosures(
	verified mediatoolchain.Verified,
	loadErr error,
	whisper whispertoolchain.Verified,
	whisperErr error,
	resources productresource.Verified,
	resourceErr error,
) (*application.ProductStatus, error) {
	reason := mediaToolUnavailableReason(loadErr)
	return application.NewProductStatus(application.ProductStatusSnapshot{
		Schema: application.ProductStatusSchema,
		Features: []application.ProductFeatureAvailability{
			mediaFeatureAvailability(
				application.FeatureAssetFrameInspection,
				verified, reason,
				mediatoolchain.CapabilityProbeV1,
				mediatoolchain.CapabilityFrameRGBV1,
			),
			mediaFeatureAvailability(
				application.FeatureSequencePreview,
				verified, reason,
				mediatoolchain.CapabilityProbeV1,
				mediatoolchain.CapabilitySourceProxyV1,
				mediatoolchain.CapabilitySequencePreviewRendererV1,
			),
			mediaFeatureAvailability(
				application.FeatureSequenceExport,
				verified, reason,
				mediatoolchain.CapabilityProbeV1,
				mediatoolchain.CapabilityRenderInputV1,
				mediatoolchain.CapabilitySequenceExportRendererV1,
			),
			mediaFeatureAvailability(
				application.FeatureSourcePreview,
				verified, reason,
				mediatoolchain.CapabilityProbeV1,
				mediatoolchain.CapabilitySourceProxyV1,
			),
			transcriptionFeatureAvailability(
				verified, reason, whisper, whisperErr, resources, resourceErr,
			),
		},
	})
}

func transcriptionFeatureAvailability(
	verified mediatoolchain.Verified,
	mediaReason application.ProductFeatureUnavailableReason,
	whisper whispertoolchain.Verified,
	whisperErr error,
	resources productresource.Verified,
	resourceErr error,
) application.ProductFeatureAvailability {
	// The normalizer is checked first and named exactly: transcription reads
	// arbitrary source audio through the media closure's ffmpeg and ffprobe
	// before whisper sees a sample. Only then do the engine and the model
	// matter.
	reason := mediaReason
	switch {
	case reason != "":
	case !hasMediaCapabilities(verified,
		mediatoolchain.CapabilityProbeV1, mediatoolchain.CapabilityFrameRGBV1):
		reason = application.ProductFeatureNotQualified
	case errors.Is(whisperErr, whispertoolchain.ErrUnavailable):
		reason = application.ProductFeatureNotInstalled
	case whisperErr != nil:
		reason = application.ProductFeatureInvalid
	case !hasWhisperCapability(whisper):
		reason = application.ProductFeatureNotQualified
	case errors.Is(resourceErr, productresource.ErrUnavailable):
		reason = application.ProductFeatureNotInstalled
	case resourceErr != nil:
		reason = application.ProductFeatureInvalid
	case !hasProductResource(resources):
		reason = application.ProductFeatureNotQualified
	}
	if reason != "" {
		return application.ProductFeatureAvailability{
			Feature: application.FeatureLocalTranscription,
			State:   application.ProductFeatureUnavailable, Reason: reason,
		}
	}
	return application.ProductFeatureAvailability{
		Feature: application.FeatureLocalTranscription, State: application.ProductFeatureAvailable,
	}
}

func hasWhisperCapability(whisper whispertoolchain.Verified) bool {
	_, exists := whisper.Capabilities[whispertoolchain.CapabilityLocalTranscriptionV1]
	return exists
}

func hasProductResource(resources productresource.Verified) bool {
	for _, entry := range resources.Entries {
		if entry.Name == application.TranscriptProfile && entry.Profile == application.TranscriptProfile {
			return true
		}
	}
	return false
}

func hasMediaCapabilities(verified mediatoolchain.Verified, required ...string) bool {
	for _, capability := range required {
		if _, exists := verified.Capabilities[capability]; !exists {
			return false
		}
	}
	return true
}

func mediaToolUnavailableReason(loadErr error) application.ProductFeatureUnavailableReason {
	if loadErr == nil {
		return ""
	}
	if errors.Is(loadErr, mediatoolchain.ErrUnavailable) {
		return application.ProductFeatureNotInstalled
	}
	return application.ProductFeatureInvalid
}

func mediaFeatureAvailability(
	feature application.ProductFeature,
	verified mediatoolchain.Verified,
	reason application.ProductFeatureUnavailableReason,
	required ...string,
) application.ProductFeatureAvailability {
	if reason == "" {
		if !hasMediaCapabilities(verified, required...) {
			reason = application.ProductFeatureNotQualified
		}
	}
	if reason != "" {
		return application.ProductFeatureAvailability{
			Feature: feature, State: application.ProductFeatureUnavailable, Reason: reason,
		}
	}
	return application.ProductFeatureAvailability{Feature: feature, State: application.ProductFeatureAvailable}
}
