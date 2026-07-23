// Package rendercontract owns the stable semantic contract shared by render
// plan compilation and the private renderer runtime. It deliberately depends
// only on product/domain so application orchestration cannot enter the
// renderer's compiled source closure.
package rendercontract

import (
	"errors"

	"github.com/PerishCode/open-cut/product/domain"
)

var ErrRenderPlanInvalid = errors.New("render plan input is invalid")

const (
	SourceProxyProfile         = "webm-vp9-opus-source-v1"
	RenderInputProfile         = "matroska-ffv1-pcm-render-input-v1"
	MaximumRenderPlanItems     = 65_536
	MaximumSourceProxyFrames   = 10_000_000
	MaximumSourceProxySamples  = 8_000_000_000
	MaximumRenderDimension     = 16_384
	MaximumPreviewVideoFrames  = 10_000_000
	MaximumPreviewAudioSamples = 2_000_000_000
)

// ExecutorIdentity is the exact private renderer provenance embedded in an
// execution manifest. It carries no application lifecycle or work ownership.
type ExecutorIdentity struct {
	Version string
	Target  string
}

func ValidFontResource(font domain.RenderFontResource) bool {
	if font.ResourceID == "" || len(font.ResourceID) > 256 || font.Version == "" || len(font.Version) > 128 {
		return false
	}
	_, err := domain.ParseDigest(font.SHA256.String())
	return err == nil
}
