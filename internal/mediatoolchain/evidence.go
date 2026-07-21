package mediatoolchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

const ConformanceEvidenceSchema = 1

type ConformanceEvidence struct {
	Schema       int                      `json:"schema"`
	Target       target.Target            `json:"target"`
	CapabilityID string                   `json:"capabilityId"`
	Profile      string                   `json:"profile"`
	SuiteSHA256  string                   `json:"suiteSha256"`
	Tools        []ConformanceDependency  `json:"tools"`
	Resources    []ConformanceDependency  `json:"resources"`
	Observations []ConformanceObservation `json:"observations"`
}

type ConformanceDependency struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type ConformanceObservation struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

func conformanceEvidenceNoticeID(capabilityID string) string {
	return "conformance-" + capabilityID
}

func conformanceSuiteDigest(capabilityID string) string {
	fixtureDigest := digestConformanceBytes(conformanceAVI())
	definitions := map[string][]string{
		CapabilityProbeV1: {
			"avi-stream-inventory-v1", "avi-duration-window-v1", "truncated-riff-rejected-v1", fixtureDigest,
		},
		CapabilityFrameRGBV1: {
			"first-frame-scale-8x8-rgb24-v1", "non-uniform-frame-v1", fixtureDigest,
		},
		CapabilitySourceProxyV1: {
			"webm-vp9-opus-source-v1", "16x16-vp9-v1", "48khz-stereo-opus-v1",
			"webm-bitexact-no-segmentuid-v1", "same-build-target-byte-stable-v1", fixtureDigest,
		},
		CapabilityRenderInputV1: {
			"matroska-ffv1-pcm-render-input-v1", "16x16-ffv1-yuv420p-v1",
			"48khz-stereo-pcm-s16-v1", "metadata-free-bitexact-matroska-v1",
			"same-build-target-byte-stable-v1", fixtureDigest,
		},
		CapabilitySequencePreviewRendererV1: {
			"vfr-floor-first-v1", "multi-track-composition-v1", "multi-track-mix-v1",
			"gap-reframe-gain-v1", "rec709-left-linear-rgba16-integer-v1",
			"millidb-q31-integer-v1", "render-bounded-streams-v2",
			"caption-language-cluster-fallback-v1",
			"av-shape-matrix-v1", "tail-padding-v1", "webm-bitexact-no-segmentuid-v1",
			"raw-evaluation-digest-v1", "verified-media-facts-v1",
			"same-build-target-byte-stable-v1",
		},
		CapabilitySequenceExportRendererV1: {
			"render-input-matroska-ffv1-pcm-v1", "vfr-floor-first-v1",
			"multi-track-composition-v1", "multi-track-mix-v1", "gap-reframe-gain-v1",
			"rec709-left-linear-rgba16-integer-v1", "millidb-q31-integer-v1",
			"render-bounded-streams-v2", "caption-language-cluster-fallback-v1",
			"av-shape-matrix-v1", "tail-padding-v1", "webm-bitexact-no-segmentuid-v1",
			"vp9-profile0-cq24-good-cpu2-thread1-v1", "opus-48khz-stereo-192k-cbr-20ms-v1",
			"raw-evaluation-digest-v1", "verified-media-facts-v1",
			"same-build-target-byte-stable-v1",
		},
	}
	definition, exists := definitions[capabilityID]
	if !exists {
		return ""
	}
	digest, err := closureDigest("open-cut/media-conformance-suite/v1", struct {
		CapabilityID string   `json:"capabilityId"`
		Checks       []string `json:"checks"`
	}{capabilityID, definition})
	if err != nil {
		return ""
	}
	return digest
}

func buildConformanceEvidence(
	buildTarget target.Target,
	capability CapabilityRecord,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
	observations []ConformanceObservation,
) (ConformanceEvidence, error) {
	evidence := ConformanceEvidence{
		Schema: ConformanceEvidenceSchema, Target: buildTarget,
		CapabilityID: capability.ID, Profile: capability.ConformanceProfile,
		SuiteSHA256:  capability.ConformanceSuiteSHA256,
		Tools:        make([]ConformanceDependency, 0, len(capability.ToolIDs)),
		Resources:    make([]ConformanceDependency, 0, len(capability.ResourceIDs)),
		Observations: append([]ConformanceObservation(nil), observations...),
	}
	for _, id := range capability.ToolIDs {
		tool, exists := tools[id]
		if !exists {
			return ConformanceEvidence{}, fmt.Errorf("conformance evidence tool is unavailable")
		}
		evidence.Tools = append(evidence.Tools, ConformanceDependency{ID: id, SHA256: tool.SHA256})
	}
	for _, id := range capability.ResourceIDs {
		resource, exists := resources[id]
		if !exists {
			return ConformanceEvidence{}, fmt.Errorf("conformance evidence resource is unavailable")
		}
		evidence.Resources = append(evidence.Resources, ConformanceDependency{ID: id, SHA256: resource.SHA256})
	}
	slices.SortFunc(evidence.Observations, func(left, right ConformanceObservation) int {
		if left.ID < right.ID {
			return -1
		}
		if left.ID > right.ID {
			return 1
		}
		return 0
	})
	if evidence.Validate() != nil {
		return ConformanceEvidence{}, fmt.Errorf("conformance evidence is invalid")
	}
	return evidence, nil
}

func (evidence ConformanceEvidence) Validate() error {
	expectedProfile, supported := capabilityConformanceProfile(evidence.CapabilityID)
	if evidence.Schema != ConformanceEvidenceSchema || evidence.Target.Validate() != nil || !supported ||
		evidence.Profile != expectedProfile || evidence.SuiteSHA256 != conformanceSuiteDigest(evidence.CapabilityID) ||
		len(evidence.Tools) == 0 || len(evidence.Tools) > 16 || len(evidence.Resources) > 16 ||
		len(evidence.Observations) == 0 || len(evidence.Observations) > 64 {
		return fmt.Errorf("conformance evidence head is invalid")
	}
	if !validConformanceDependencies(evidence.Tools) || !validConformanceDependencies(evidence.Resources) {
		return fmt.Errorf("conformance evidence dependency is invalid")
	}
	previous := ""
	for _, observation := range evidence.Observations {
		if !identifier.MatchString(observation.ID) || observation.ID <= previous || !validDigest(observation.SHA256) {
			return fmt.Errorf("conformance evidence observation is invalid")
		}
		previous = observation.ID
	}
	return nil
}

func validConformanceDependencies(values []ConformanceDependency) bool {
	previous := ""
	for _, value := range values {
		if !identifier.MatchString(value.ID) || value.ID <= previous || !validDigest(value.SHA256) {
			return false
		}
		previous = value.ID
	}
	return true
}

func readConformanceEvidence(path string) (ConformanceEvidence, error) {
	file, err := os.Open(path)
	if err != nil {
		return ConformanceEvidence{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumManifestBytes+1))
	if err != nil || len(data) == 0 || len(data) > maximumManifestBytes {
		return ConformanceEvidence{}, fmt.Errorf("conformance evidence size is invalid")
	}
	var evidence ConformanceEvidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		evidence.Validate() != nil {
		return ConformanceEvidence{}, fmt.Errorf("conformance evidence is invalid")
	}
	return evidence, nil
}

func writeConformanceEvidence(
	stageRoot string,
	evidence ConformanceEvidence,
) (NoticeRecord, error) {
	if evidence.Validate() != nil {
		return NoticeRecord{}, fmt.Errorf("conformance evidence is invalid")
	}
	relative := filepath.ToSlash(filepath.Join(
		"licenses", "media", "conformance", evidence.CapabilityID+".json",
	))
	filename := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := atomicfile.WriteJSON(filename, evidence, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	digest, size, err := digestFile(filename)
	if err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{
		ID:   conformanceEvidenceNoticeID(evidence.CapabilityID),
		Path: relative, SHA256: digest, ByteSize: size,
	}, nil
}

func digestConformanceBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func digestConformanceJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return digestConformanceBytes(encoded)
}

func capabilityRecord(records []CapabilityRecord, id string) CapabilityRecord {
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	return CapabilityRecord{}
}

func conformanceEvidenceEqual(left, right ConformanceEvidence) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}
