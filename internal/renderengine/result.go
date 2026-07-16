package renderengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	ResultSchema       = 3
	MaximumResultBytes = 64 << 10
	ResultFilename     = "result.json"

	ResultCodePlanInvalid      = "render-plan-invalid"
	ResultCodeInputUnavailable = "render-input-unavailable"
	ResultCodeFontUnavailable  = "render-font-unavailable"
	ResultCodeGlyphMissing     = "render-glyph-missing"
	ResultCodeColorEmoji       = "render-color-emoji-unsupported"
	ResultCodeStorage          = "render-storage-insufficient"
	ResultCodeResourceLimit    = "render-resource-limit-exceeded"
	ResultCodeDecode           = "render-decode-failed"
	ResultCodeEncode           = "render-encode-failed"
	ResultCodeInternal         = "renderer-internal-failed"
)

type ResultStatus string

const (
	ResultSucceeded ResultStatus = "success"
	ResultFailed    ResultStatus = "failed"
)

type ResultDocument struct {
	Schema     int               `json:"schema"`
	Status     ResultStatus      `json:"status"`
	Diagnostic *ResultDiagnostic `json:"diagnostic,omitempty"`
	Evaluation *ResultEvaluation `json:"evaluation,omitempty"`
	Output     *ResultOutput     `json:"output,omitempty"`
}

type ResultDiagnostic struct {
	Code        string `json:"code"`
	SubjectKind string `json:"subjectKind,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
}

type ResultOutput struct {
	RelativePath string        `json:"relativePath"`
	ByteSize     domain.UInt64 `json:"byteSize"`
	SHA256       domain.Digest `json:"sha256"`
}

type ResultEvaluation struct {
	Video ResultStreamObservation `json:"video"`
	Audio ResultStreamObservation `json:"audio"`
}

type ResultStreamObservation struct {
	ByteSize domain.UInt64 `json:"byteSize"`
	SHA256   domain.Digest `json:"sha256"`
}

var resultSubjectID = regexp.MustCompile(`^[[:graph:] ]{1,256}$`)

func EncodeResult(result ResultDocument) ([]byte, error) {
	if result.Validate() != nil {
		return nil, fmt.Errorf("render result is invalid")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) == 0 || len(encoded) > MaximumResultBytes {
		return nil, fmt.Errorf("render result exceeds its bound")
	}
	return encoded, nil
}

func DecodeResult(data []byte) (ResultDocument, error) {
	if len(data) == 0 || len(data) > MaximumResultBytes {
		return ResultDocument{}, fmt.Errorf("render result size is invalid")
	}
	var result ResultDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		result.Validate() != nil {
		return ResultDocument{}, fmt.Errorf("render result is invalid")
	}
	return result, nil
}

func (result ResultDocument) Validate() error {
	if result.Schema != ResultSchema {
		return fmt.Errorf("render result schema is invalid")
	}
	switch result.Status {
	case ResultSucceeded:
		if result.Diagnostic != nil || result.Output == nil || result.Evaluation == nil ||
			!validResultOutputPath(result.Output.RelativePath) || result.Output.ByteSize.Value() == 0 ||
			!validDigest(result.Output.SHA256) || result.Evaluation.Validate() != nil {
			return fmt.Errorf("render success result is invalid")
		}
	case ResultFailed:
		if result.Output != nil || result.Evaluation != nil || result.Diagnostic == nil ||
			result.Diagnostic.Validate() != nil {
			return fmt.Errorf("render failure result is invalid")
		}
	default:
		return fmt.Errorf("render result status is invalid")
	}
	return nil
}

func validResultOutputPath(value string) bool {
	return value == "preview.webm" || value == "export.webm"
}

func (evaluation ResultEvaluation) Validate() error {
	for _, stream := range []ResultStreamObservation{evaluation.Video, evaluation.Audio} {
		if stream.ByteSize.Value() == 0 || !validDigest(stream.SHA256) {
			return fmt.Errorf("render evaluation stream is invalid")
		}
	}
	return nil
}

func (diagnostic ResultDiagnostic) Validate() error {
	if !validResultCode(diagnostic.Code) || (diagnostic.SubjectKind == "") != (diagnostic.SubjectID == "") {
		return fmt.Errorf("render diagnostic is invalid")
	}
	if diagnostic.SubjectKind == "" {
		return nil
	}
	switch diagnostic.SubjectKind {
	case "plan", "input", "resource", "caption", "tool", "storage", "output":
	default:
		return fmt.Errorf("render diagnostic subject kind is invalid")
	}
	if !utf8.ValidString(diagnostic.SubjectID) || !resultSubjectID.MatchString(diagnostic.SubjectID) {
		return fmt.Errorf("render diagnostic subject is invalid")
	}
	for _, current := range diagnostic.SubjectID {
		if unicode.IsControl(current) {
			return fmt.Errorf("render diagnostic subject is invalid")
		}
	}
	return nil
}

func validResultCode(code string) bool {
	switch code {
	case ResultCodePlanInvalid, ResultCodeInputUnavailable, ResultCodeFontUnavailable,
		ResultCodeGlyphMissing, ResultCodeColorEmoji, ResultCodeStorage,
		ResultCodeResourceLimit, ResultCodeDecode, ResultCodeEncode, ResultCodeInternal:
		return true
	default:
		return false
	}
}
