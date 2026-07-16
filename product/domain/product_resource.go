package domain

import (
	"errors"
	"regexp"
	"time"
)

const ProductResourceSchema = "open-cut/product-resource/v1"

var (
	ErrInvalidProductResource = errors.New("invalid product resource")
	productResourceName       = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,127}$`)
)

type ProductResourceKind string

const ProductResourceTranscriptionModel ProductResourceKind = "transcription-model"

type ProductResourceRetention string

const ProductResourceRetentionOffline ProductResourceRetention = "offline"

type ProductResource struct {
	ID             ResourceID               `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	InstallationID string                   `json:"-"`
	Name           string                   `json:"name" pattern:"^[a-z][a-z0-9.-]{0,127}$"`
	Kind           ProductResourceKind      `json:"kind" enum:"transcription-model"`
	Version        string                   `json:"version" minLength:"1" maxLength:"128"`
	Profile        string                   `json:"profile" minLength:"1" maxLength:"128"`
	EntryDigest    Digest                   `json:"-"`
	ContentDigest  Digest                   `json:"-"`
	ByteSize       UInt64                   `json:"byteSize" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Retention      ProductResourceRetention `json:"retention" enum:"offline"`
	CreatedAt      time.Time                `json:"createdAt"`
}

func (resource ProductResource) Validate() error {
	if resource.ID.IsZero() || !productResourceName.MatchString(resource.Name) ||
		!validMediaText(resource.InstallationID, 128, true) ||
		!validMediaText(resource.Version, 128, true) || !validMediaText(resource.Profile, 128, true) ||
		resource.Kind != ProductResourceTranscriptionModel ||
		resource.Retention != ProductResourceRetentionOffline || resource.ByteSize.Value() == 0 ||
		resource.CreatedAt.IsZero() {
		return ErrInvalidProductResource
	}
	if _, err := ParseDigest(resource.EntryDigest.String()); err != nil {
		return ErrInvalidProductResource
	}
	if _, err := ParseDigest(resource.ContentDigest.String()); err != nil {
		return ErrInvalidProductResource
	}
	return nil
}

func ValidProductResourceName(value string) bool {
	return productResourceName.MatchString(value)
}
