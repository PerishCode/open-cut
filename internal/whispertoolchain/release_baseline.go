package whispertoolchain

import (
	"fmt"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
)

// ReleaseBaselineProfile is this closure's own publication policy.
//
// Transcription used to be a line in the media toolchain's baseline. It is
// stated here instead because the requirement belongs to whoever builds the
// engine — and because a media baseline could not express what this one can:
// every public target must ship a qualified engine, but the backend it was
// qualified against is legitimately per target. Absence of Metal on Windows is
// not a missing capability; it is a different, still-qualified one.
const ReleaseBaselineProfile = "desktop-creator-transcription-v1"

// VerifyReleaseBaseline is the app-owned production policy for the whisper
// closure. Load must have established the verified byte closure first, and the
// caller must still replay VerifyCapabilities before publication.
func VerifyReleaseBaseline(verified Verified) error {
	capability, exists := verified.Capabilities[CapabilityLocalTranscriptionV1]
	if !exists || capability.ID != CapabilityLocalTranscriptionV1 ||
		!toolchainclosure.CleanAbsolute(capability.Entry.Path) ||
		!toolchainclosure.ValidDigest(capability.ClosureSHA256) ||
		capability.ConformanceProfile == "" {
		return fmt.Errorf(
			"release baseline %s requires qualified %s",
			ReleaseBaselineProfile, CapabilityLocalTranscriptionV1,
		)
	}
	// The backend must be one this build target actually offers. A closure
	// claiming an accelerated backend on a target that has none would pass
	// every byte check while describing a build that cannot exist.
	if verified.Manifest.Build.Backend != Backend(verified.Manifest.Target) {
		return fmt.Errorf(
			"release baseline %s requires the %s backend on %s",
			ReleaseBaselineProfile, Backend(verified.Manifest.Target), verified.Manifest.Target,
		)
	}
	return nil
}
