package mediatoolchain

import (
	"context"
	"fmt"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	RendererRelinkQualificationReceiptName = "media-renderer-relink.qualification.json"
	rendererRelinkQualificationProfile     = "renderer-relink-v1"
	rendererRelinkQualificationDomain      = "open-cut/media-renderer-relink-qualification/v1"
)

type rendererRelinkQualificationCapability struct {
	ID            string `json:"id"`
	ClosureSHA256 string `json:"closureSha256"`
	SuiteSHA256   string `json:"suiteSha256"`
}

type rendererRelinkQualificationInput struct {
	Schema         int                                     `json:"schema"`
	Target         target.Target                           `json:"target"`
	ToolchainID    string                                  `json:"toolchainId"`
	Version        string                                  `json:"version"`
	RecipeSHA256   string                                  `json:"recipeSha256"`
	ReleaseProfile string                                  `json:"releaseProfile"`
	Renderer       RendererBuildRecord                     `json:"renderer"`
	RelinkNotice   NoticeRecord                            `json:"relinkNotice"`
	Capabilities   []rendererRelinkQualificationCapability `json:"capabilities"`
}

// EnsureRendererRelinkQualification reuses a receipt only when it binds the
// exact renderer, relink kit, native inputs, smoke evidence, and current
// qualification contract. Any doubt replays the real relink before replacing
// the receipt.
func EnsureRendererRelinkQualification(ctx context.Context, verified Verified) (bool, error) {
	if err := VerifyReleaseBaseline(verified); err != nil {
		return false, err
	}
	if err := VerifyRendererRelinkQualificationReceipt(verified); err == nil {
		return true, nil
	}
	if err := VerifyRendererRelink(ctx, verified); err != nil {
		return false, err
	}
	if err := writeRendererRelinkQualificationReceipt(verified); err != nil {
		return false, err
	}
	return false, nil
}

func VerifyRendererRelinkQualificationReceipt(verified Verified) error {
	input, err := rendererRelinkQualificationIdentity(verified)
	if err != nil {
		return err
	}
	return toolchainclosure.VerifyQualificationReceipt(
		verified.Root, RendererRelinkQualificationReceiptName,
		rendererRelinkQualificationProfile, rendererRelinkQualificationDomain, input,
	)
}

func writeRendererRelinkQualificationReceipt(verified Verified) error {
	input, err := rendererRelinkQualificationIdentity(verified)
	if err != nil {
		return err
	}
	return toolchainclosure.WriteQualificationReceipt(
		verified.Root, RendererRelinkQualificationReceiptName,
		rendererRelinkQualificationProfile, rendererRelinkQualificationDomain, input,
	)
}

func rendererRelinkQualificationIdentity(verified Verified) (rendererRelinkQualificationInput, error) {
	manifest := verified.Manifest
	if manifest.Build.Renderer == nil || verified.Root == "" {
		return rendererRelinkQualificationInput{}, fmt.Errorf("renderer relink qualification input is unavailable")
	}
	notice := noticeRecord(manifest.Notices, manifest.Build.Renderer.RelinkNoticeID)
	if notice.ID == "" {
		return rendererRelinkQualificationInput{}, fmt.Errorf("renderer relink qualification notice is unavailable")
	}
	capabilities := make([]rendererRelinkQualificationCapability, 0, 2)
	for _, id := range []string{
		CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1,
	} {
		capability, exists := verified.Capabilities[id]
		if !exists {
			return rendererRelinkQualificationInput{}, fmt.Errorf("renderer relink qualification capability %s is unavailable", id)
		}
		capabilities = append(capabilities, rendererRelinkQualificationCapability{
			ID: id, ClosureSHA256: capability.ClosureSHA256,
			SuiteSHA256: capability.ConformanceSuiteSHA256,
		})
	}
	return rendererRelinkQualificationInput{
		Schema: 1, Target: manifest.Target, ToolchainID: manifest.ToolchainID,
		Version: manifest.Version, RecipeSHA256: manifest.Build.RecipeSHA256,
		ReleaseProfile: ReleaseBaselineProfile, Renderer: *manifest.Build.Renderer,
		RelinkNotice: notice, Capabilities: capabilities,
	}, nil
}

// RendererRelinkQualificationContractDigest lets a cache key follow the exact
// verification contract even though the receipt itself is produced later.
func RendererRelinkQualificationContractDigest(buildTarget target.Target) string {
	digest, err := toolchainclosure.ClosureDigest(
		rendererRelinkQualificationDomain+"/contract",
		struct {
			Schema         int           `json:"schema"`
			Target         target.Target `json:"target"`
			Profile        string        `json:"profile"`
			ReleaseProfile string        `json:"releaseProfile"`
			Suites         []string      `json:"suites"`
		}{
			Schema: 1, Target: buildTarget, Profile: rendererRelinkQualificationProfile,
			ReleaseProfile: ReleaseBaselineProfile,
			Suites: []string{
				conformanceSuiteDigest(CapabilitySequencePreviewRendererV1),
				conformanceSuiteDigest(CapabilitySequenceExportRendererV1),
			},
		},
	)
	if err != nil {
		return ""
	}
	return digest
}
