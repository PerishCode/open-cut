package service

import (
	"errors"

	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/product/application"
)

func NewProductStatusFromMediaTools(
	verified mediatoolchain.Verified,
	loadErr error,
) (*application.ProductStatus, error) {
	return NewProductStatusFromClosures(
		verified, loadErr, productresource.Verified{}, productresource.ErrUnavailable,
	)
}

func NewProductStatusFromClosures(
	verified mediatoolchain.Verified,
	loadErr error,
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
			transcriptionFeatureAvailability(verified, reason, resources, resourceErr),
		},
	})
}

func transcriptionFeatureAvailability(
	verified mediatoolchain.Verified,
	mediaReason application.ProductFeatureUnavailableReason,
	resources productresource.Verified,
	resourceErr error,
) application.ProductFeatureAvailability {
	reason := mediaReason
	if reason == "" {
		switch {
		case errors.Is(resourceErr, productresource.ErrUnavailable):
			reason = application.ProductFeatureNotInstalled
		case resourceErr != nil:
			reason = application.ProductFeatureInvalid
		case !hasProductResource(resources):
			reason = application.ProductFeatureNotQualified
		case !hasMediaCapabilities(verified, mediatoolchain.CapabilityLocalTranscriptionV1):
			reason = application.ProductFeatureNotQualified
		}
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
