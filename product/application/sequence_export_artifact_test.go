package application

import (
	"bytes"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequenceExportArtifactManifestIsCanonicalAndBindsVerifiedOutput(t *testing.T) {
	compiled, err := CompileSequenceExportPlan(renderExportPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := SequenceExportFactsForPlan(compiled.Plan.Payload)
	if err != nil {
		t.Fatal(err)
	}
	job := mustRenderID(t, "00000000-0000-7000-8000-000000000071", domain.ParseWorkJobID)
	size, _ := domain.NewUInt64(4096)
	manifest := SequenceExportArtifactManifest{
		ProducerJobID: job, ProjectID: compiled.Plan.Payload.ProjectID,
		SequenceID:       compiled.Plan.Payload.SequenceID,
		SequenceRevision: compiled.Plan.Payload.SequenceRevision,
		RenderPlanDigest: compiled.Plan.Digest, RendererVersion: "renderer-fixture-v1",
		RendererTarget: "mac-arm64", Profile: domain.SequenceExportProfileV1, Facts: facts,
		Media: SequenceExportArtifactFile{
			Path: "export.webm", MimeType: "video/webm", ByteSize: size,
			SHA256: renderDigest("e"),
		},
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-artifact", domain.SequenceExportArtifactSchema, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSequenceExportArtifactManifest(canonical)
	if err != nil || decoded != manifest {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	reencoded, repeatedDigest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-artifact", domain.SequenceExportArtifactSchema, decoded,
	)
	if err != nil || repeatedDigest != digest || !bytes.Equal(reencoded, canonical) {
		t.Fatalf("canonical drift digest=%s repeated=%s err=%v", digest, repeatedDigest, err)
	}
	unknown := bytes.Replace(canonical, []byte(`"path":"export.webm"`),
		[]byte(`"ambient":true,"path":"export.webm"`), 1)
	if _, err := DecodeSequenceExportArtifactManifest(unknown); err == nil {
		t.Fatal("unknown export manifest field was accepted")
	}
}

func mustRenderID[T any](t *testing.T, value string, parse func(string) (T, error)) T {
	t.Helper()
	result, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
