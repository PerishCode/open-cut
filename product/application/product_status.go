package application

import (
	"context"
	"errors"
	"fmt"
)

const ProductStatusSchema = "open-cut/product-status/v1"

var ErrProductStatusInvalid = errors.New("product status is invalid")

type ProductFeature string

const (
	FeatureAssetFrameInspection ProductFeature = "asset-frame-inspection"
	FeatureSequencePreview      ProductFeature = "sequence-preview"
	FeatureSequenceExport       ProductFeature = "sequence-export"
	FeatureSourcePreview        ProductFeature = "source-preview"
	FeatureLocalTranscription   ProductFeature = "local-transcription"
)

type ProductFeatureState string

const (
	ProductFeatureAvailable   ProductFeatureState = "available"
	ProductFeatureUnavailable ProductFeatureState = "unavailable"
)

type ProductFeatureUnavailableReason string

const (
	ProductFeatureNotInstalled ProductFeatureUnavailableReason = "not-installed"
	ProductFeatureNotQualified ProductFeatureUnavailableReason = "not-qualified"
	ProductFeatureInvalid      ProductFeatureUnavailableReason = "invalid-closure"
)

type ProductFeatureAvailability struct {
	Feature ProductFeature                  `json:"feature" enum:"asset-frame-inspection,sequence-preview,sequence-export,source-preview,local-transcription"`
	State   ProductFeatureState             `json:"state" enum:"available,unavailable"`
	Reason  ProductFeatureUnavailableReason `json:"reason,omitempty" enum:"not-installed,not-qualified,invalid-closure"`
}

type ProductStatusSnapshot struct {
	Schema   string                       `json:"schema" enum:"open-cut/product-status/v1"`
	Features []ProductFeatureAvailability `json:"features" minItems:"5" maxItems:"5" nullable:"false"`
}

func (snapshot ProductStatusSnapshot) Validate() error {
	expected := [...]ProductFeature{
		FeatureAssetFrameInspection,
		FeatureSequencePreview,
		FeatureSequenceExport,
		FeatureSourcePreview,
		FeatureLocalTranscription,
	}
	if snapshot.Schema != ProductStatusSchema || len(snapshot.Features) != len(expected) {
		return ErrProductStatusInvalid
	}
	for index, feature := range snapshot.Features {
		if feature.Feature != expected[index] {
			return ErrProductStatusInvalid
		}
		switch feature.State {
		case ProductFeatureAvailable:
			if feature.Reason != "" {
				return ErrProductStatusInvalid
			}
		case ProductFeatureUnavailable:
			if feature.Reason != ProductFeatureNotInstalled &&
				feature.Reason != ProductFeatureNotQualified && feature.Reason != ProductFeatureInvalid {
				return ErrProductStatusInvalid
			}
		default:
			return ErrProductStatusInvalid
		}
	}
	return nil
}

type ProductStatus struct {
	snapshot ProductStatusSnapshot
}

func NewProductStatus(snapshot ProductStatusSnapshot) (*ProductStatus, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("create product status: %w", err)
	}
	snapshot.Features = append([]ProductFeatureAvailability(nil), snapshot.Features...)
	return &ProductStatus{snapshot: snapshot}, nil
}

func (status *ProductStatus) Read(ctx context.Context) (ProductStatusSnapshot, error) {
	if status == nil {
		return ProductStatusSnapshot{}, ErrProductStatusInvalid
	}
	if _, err := AuthorityFromContext(ctx); err != nil {
		return ProductStatusSnapshot{}, err
	}
	result := status.snapshot
	result.Features = append([]ProductFeatureAvailability(nil), result.Features...)
	return result, nil
}
