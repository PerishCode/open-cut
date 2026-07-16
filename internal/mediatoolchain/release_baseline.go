package mediatoolchain

import "fmt"

const ReleaseBaselineProfile = "desktop-creator-v1"

var releaseBaselineCapabilities = [...]string{
	CapabilityProbeV1,
	CapabilityFrameRGBV1,
	CapabilitySourceProxyV1,
	CapabilityRenderInputV1,
	CapabilitySequencePreviewRendererV1,
	CapabilitySequenceExportRendererV1,
	CapabilityLocalTranscriptionV1,
}

// VerifyReleaseBaseline is the app-owned production artifact policy. Load must
// establish the verified byte closure before this declaration gate, and the
// caller must still replay VerifyCapabilities before publication. The split
// prevents a valid but deliberately reduced development catalog from becoming
// a published desktop creator payload without wasting a full conformance run.
func VerifyReleaseBaseline(verified Verified) error {
	for _, id := range releaseBaselineCapabilities {
		capability, exists := verified.Capabilities[id]
		if !exists || capability.ID != id || !cleanAbsolute(capability.Entry.Path) ||
			!validDigest(capability.ClosureSHA256) || capability.ConformanceProfile == "" {
			return fmt.Errorf("release baseline %s requires qualified %s", ReleaseBaselineProfile, id)
		}
	}
	return nil
}
