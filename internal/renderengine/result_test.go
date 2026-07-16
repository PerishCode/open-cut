package renderengine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestRenderResultClosesSuccessAndTypedFailureShapes(t *testing.T) {
	size, _ := domain.NewUInt64(123)
	success := ResultDocument{
		Schema: ResultSchema, Status: ResultSucceeded,
		Evaluation: &ResultEvaluation{
			Video: resultStreamObservation("c", 384), Audio: resultStreamObservation("d", 192_000),
		},
		Output: &ResultOutput{
			RelativePath: "preview.webm", ByteSize: size,
			SHA256: domain.Digest("sha256:" + strings.Repeat("a", 64)),
		},
	}
	encoded, err := EncodeResult(success)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeResult(encoded)
	if err != nil || decoded.Output == nil || decoded.Output.SHA256 != success.Output.SHA256 {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	exportSuccess := success
	exportOutput := *success.Output
	exportOutput.RelativePath = "export.webm"
	exportSuccess.Output = &exportOutput
	if _, err := EncodeResult(exportSuccess); err != nil {
		t.Fatalf("export success result was rejected: %v", err)
	}
	failure := ResultDocument{
		Schema: ResultSchema, Status: ResultFailed,
		Diagnostic: &ResultDiagnostic{
			Code: ResultCodeGlyphMissing, SubjectKind: "caption",
			SubjectID: "00000000-0000-7000-8000-000000000004",
		},
	}
	encoded, err = EncodeResult(failure)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeResult(encoded)
	if err != nil || decoded.Diagnostic == nil || decoded.Diagnostic.Code != ResultCodeGlyphMissing {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	unknown := bytes.Replace(encoded, []byte(ResultCodeGlyphMissing), []byte("ambient-failure"), 1)
	if _, err := DecodeResult(unknown); err == nil {
		t.Fatal("unknown helper failure code was accepted")
	}
	resourceLimit := ResultDocument{
		Schema: ResultSchema, Status: ResultFailed,
		Diagnostic: &ResultDiagnostic{
			Code: ResultCodeResourceLimit, SubjectKind: "plan", SubjectID: "pixel-sample-work",
		},
	}
	if _, err := EncodeResult(resourceLimit); err != nil {
		t.Fatalf("resource limit failure was rejected: %v", err)
	}
}

func TestRenderResultRejectsMixedAndUnknownShapes(t *testing.T) {
	size, _ := domain.NewUInt64(1)
	output := &ResultOutput{
		RelativePath: "preview.webm", ByteSize: size,
		SHA256: domain.Digest("sha256:" + strings.Repeat("b", 64)),
	}
	evaluation := &ResultEvaluation{
		Video: resultStreamObservation("c", 1), Audio: resultStreamObservation("d", 1),
	}
	diagnostic := &ResultDiagnostic{Code: ResultCodeInternal}
	for _, result := range []ResultDocument{
		{Schema: ResultSchema, Status: ResultSucceeded, Output: output, Evaluation: evaluation, Diagnostic: diagnostic},
		{Schema: ResultSchema, Status: ResultSucceeded, Output: output},
		{Schema: ResultSchema, Status: ResultFailed, Output: output, Evaluation: evaluation, Diagnostic: diagnostic},
		{Schema: ResultSchema, Status: ResultFailed, Diagnostic: &ResultDiagnostic{
			Code: ResultCodeGlyphMissing, SubjectKind: "caption",
		}},
		{Schema: ResultSchema, Status: "ambient", Output: output},
	} {
		if _, err := EncodeResult(result); err == nil {
			t.Fatalf("invalid result was encoded: %+v", result)
		}
	}
}

func resultStreamObservation(fill string, size uint64) ResultStreamObservation {
	value, _ := domain.NewUInt64(size)
	return ResultStreamObservation{
		ByteSize: value, SHA256: domain.Digest("sha256:" + strings.Repeat(fill, 64)),
	}
}
