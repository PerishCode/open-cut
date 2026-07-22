package mediatoolchain

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestRendererRelinkQualificationReceiptBindsExactClosure(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verified := Verified{
		Root: root,
		Manifest: Manifest{
			Target: target.Host(), ToolchainID: "ffmpeg", Version: toolchainVersion,
			Build: BuildRecord{RecipeSHA256: digest, Renderer: fixtureRendererBuildRecord()},
			Notices: []NoticeRecord{{
				ID: RendererRelinkNoticeID, Path: "relink.tar.zst", SHA256: digest, ByteSize: 1,
			}},
		},
		Capabilities: map[string]Capability{
			CapabilitySequencePreviewRendererV1: {
				ID: CapabilitySequencePreviewRendererV1, ClosureSHA256: digest,
				ConformanceSuiteSHA256: conformanceSuiteDigest(CapabilitySequencePreviewRendererV1),
			},
			CapabilitySequenceExportRendererV1: {
				ID: CapabilitySequenceExportRendererV1, ClosureSHA256: digest,
				ConformanceSuiteSHA256: conformanceSuiteDigest(CapabilitySequenceExportRendererV1),
			},
		},
	}
	if err := writeRendererRelinkQualificationReceipt(verified); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRendererRelinkQualificationReceipt(verified); err != nil {
		t.Fatal(err)
	}
	changed := verified
	changed.Manifest.Build.RecipeSHA256 = "sha256:" + strings.Repeat("b", 64)
	if err := VerifyRendererRelinkQualificationReceipt(changed); err == nil {
		t.Fatal("renderer receipt accepted a different build recipe")
	}
}

func TestRendererRelinkQualificationContractDigestIsTargetScoped(t *testing.T) {
	mac := RendererRelinkQualificationContractDigest(target.Target{Platform: target.Mac, Arch: target.ARM64})
	linux := RendererRelinkQualificationContractDigest(target.Target{Platform: target.Linux, Arch: target.X64})
	if !toolchainclosure.ValidDigest(mac) || !toolchainclosure.ValidDigest(linux) || mac == linux {
		t.Fatalf("qualification contract digests mac=%q linux=%q", mac, linux)
	}
}
