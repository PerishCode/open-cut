package whispertoolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	ConformanceEvidenceSchema = 1

	maximumConformanceJSONBytes = 16 << 20
	maximumEvidenceBytes        = 64 << 10
)

type ConformanceEvidence struct {
	Schema       int                      `json:"schema"`
	Target       target.Target            `json:"target"`
	Backend      string                   `json:"backend"`
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

// conformanceSuiteDigest binds a suite identity to the build target.
//
// This is the correctness point the media toolchain's equivalent misses: its
// suite digest hashes only the capability id and the check list, which is safe
// only while every target exercises the same code path. Once a capability is
// qualified against different backends per target, a target-blind suite digest
// would let two materially different qualifications claim the same identity.
func conformanceSuiteDigest(capabilityID string, buildTarget target.Target) string {
	if capabilityID != CapabilityLocalTranscriptionV1 {
		return ""
	}
	checks := []string{
		"canonical-16khz-mono-s16-fixture-v1",
		"strict-json-full-result-v1",
		"repeated-run-semantic-stability-v1",
		"malformed-model-rejected-v1",
		digestConformanceBytes(conformanceWAV()),
	}
	digest, err := toolchainclosure.ClosureDigest("open-cut/whisper-conformance-suite/v1", struct {
		CapabilityID string        `json:"capabilityId"`
		Target       target.Target `json:"target"`
		Backend      string        `json:"backend"`
		Checks       []string      `json:"checks"`
	}{capabilityID, buildTarget, Backend(buildTarget), checks})
	if err != nil {
		return ""
	}
	return digest
}

// conformanceWAV synthesizes the canonical qualification fixture directly: a
// one-second 16 kHz mono S16 WAV.
//
// The media toolchain decoded and resampled an AVI with FFmpeg to reach this
// format. Doing so here would drag the entire FFmpeg closure back into a
// toolchain whose runtime does not use it, purely to manufacture bytes that Go
// can produce deterministically. The API already normalizes real audio to this
// exact shape before whisper is ever invoked, so the fixture matches what the
// capability actually receives.
func conformanceWAV() []byte {
	const (
		sampleRate = 16_000
		channels   = 1
		bits       = 16
	)
	pcm := new(bytes.Buffer)
	for sample := 0; sample < sampleRate; sample++ {
		phase := sample % 80
		value := int16(12_000)
		if phase >= 40 {
			value = -value
		}
		_ = binary.Write(pcm, binary.LittleEndian, value)
	}
	payload := pcm.Bytes()
	blockAlign := channels * bits / 8
	result := new(bytes.Buffer)
	result.WriteString("RIFF")
	_ = binary.Write(result, binary.LittleEndian, uint32(len(payload)+36))
	result.WriteString("WAVE")
	result.WriteString("fmt ")
	_ = binary.Write(result, binary.LittleEndian, uint32(16))
	_ = binary.Write(result, binary.LittleEndian, uint16(1))
	_ = binary.Write(result, binary.LittleEndian, uint16(channels))
	_ = binary.Write(result, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(result, binary.LittleEndian, uint32(sampleRate*blockAlign))
	_ = binary.Write(result, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(result, binary.LittleEndian, uint16(bits))
	result.WriteString("data")
	_ = binary.Write(result, binary.LittleEndian, uint32(len(payload)))
	result.Write(payload)
	return result.Bytes()
}

func capabilityRecord(
	notices []NoticeRecord, whisperNotice NoticeRecord, model ResourceRecord,
	buildTarget target.Target,
) CapabilityRecord {
	noticeIDs := make([]string, 0, len(notices)+2)
	for _, notice := range notices {
		noticeIDs = append(noticeIDs, notice.ID)
	}
	noticeIDs = append(noticeIDs, whisperNotice.ID)
	evidenceID := conformanceEvidenceNoticeID(CapabilityLocalTranscriptionV1)
	noticeIDs = append(noticeIDs, evidenceID)
	slices.Sort(noticeIDs)
	noticeIDs = slices.Compact(noticeIDs)
	return CapabilityRecord{
		ID: CapabilityLocalTranscriptionV1, EntryToolID: ToolWhisperCLI,
		ToolIDs: []string{ToolWhisperCLI}, ResourceIDs: []string{model.ID},
		NoticeIDs: noticeIDs, ConformanceProfile: ConformanceLocalTranscriptionV1,
		ConformanceSuiteSHA256:      conformanceSuiteDigest(CapabilityLocalTranscriptionV1, buildTarget),
		ConformanceEvidenceNoticeID: evidenceID,
	}
}

// Qualify proves the capability on this machine. The contract is semantic
// stability, not byte reproducibility: whisper must produce the same result
// twice on the same host, and must reject a model that is not one.
func Qualify(ctx context.Context, whisperPath, modelPath string) ([]ConformanceObservation, error) {
	root, err := os.MkdirTemp("", "open-cut-whisper-conformance-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	fixture := conformanceWAV()
	wavPath := filepath.Join(root, "conformance.wav")
	if err := os.WriteFile(wavPath, fixture, 0o600); err != nil {
		return nil, err
	}
	first, err := runWhisper(ctx, whisperPath, modelPath, root, wavPath, "first")
	if err != nil {
		return nil, err
	}
	second, err := runWhisper(ctx, whisperPath, modelPath, root, wavPath, "second")
	if err != nil {
		return nil, err
	}
	firstDigest := digestSemanticResult(first)
	if firstDigest == "" || digestSemanticResult(second) != firstDigest {
		return nil, fmt.Errorf("local transcription semantic output is not stable")
	}
	malformed := filepath.Join(root, "malformed-model.bin")
	if err := os.WriteFile(malformed, []byte("not-a-whisper-model"), 0o600); err != nil {
		return nil, err
	}
	if _, err := runWhisper(ctx, whisperPath, malformed, root, wavPath, "malformed"); err == nil {
		return nil, fmt.Errorf("local transcription accepted a malformed model")
	}
	return []ConformanceObservation{
		{ID: "canonical-fixture", SHA256: digestConformanceBytes(fixture)},
		{ID: "malformed-model", SHA256: digestConformanceBytes([]byte("rejected"))},
		{ID: "semantic-result", SHA256: firstDigest},
	}, nil
}

// whisperResult is the decoded shape of whisper's strict JSON-full output.
//
// systeminfo is deliberately absent. whisper embeds a build capability banner
// there ("MTL : EMBED_LIBRARY = 1 | ... ACCELERATE = 1"), so including it would
// make the semantic digest a function of compile flags — every backend change
// would look like an output change, and the stability check would be proving
// the banner rather than the transcription.
type whisperResult struct {
	Transcription []whisperSegment `json:"transcription"`
}

type whisperSegment struct {
	Timestamps struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"timestamps"`
	Offsets struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	} `json:"offsets"`
	Text string `json:"text"`
}

func runWhisper(
	ctx context.Context, whisperPath, modelPath, directory, wavPath, label string,
) (whisperResult, error) {
	prefix := filepath.Join(directory, label)
	stderr := &limitedBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	args := []string{
		"-m", modelPath, "-f", wavPath, "-l", "auto", "-ojf", "-of", prefix,
		"-np", "-t", "1", "-p", "1", "-nf", "-sow",
	}
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: whisperPath, Args: args, Directory: directory,
		Env: conformanceEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil {
		return whisperResult{}, fmt.Errorf("run whisper conformance %s: %v: %s", label, err, stderr.String())
	}
	return readWhisperResult(prefix + ".json")
}

func readWhisperResult(path string) (whisperResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return whisperResult{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumConformanceJSONBytes+1))
	if err != nil || len(data) == 0 || len(data) > maximumConformanceJSONBytes {
		return whisperResult{}, fmt.Errorf("whisper conformance result size is invalid")
	}
	var result whisperResult
	if err := json.Unmarshal(data, &result); err != nil {
		return whisperResult{}, fmt.Errorf("decode whisper conformance result")
	}
	return result, nil
}

func buildConformanceEvidence(
	buildTarget target.Target,
	capability CapabilityRecord,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
	observations []ConformanceObservation,
) (ConformanceEvidence, error) {
	evidence := ConformanceEvidence{
		Schema: ConformanceEvidenceSchema, Target: buildTarget, Backend: Backend(buildTarget),
		CapabilityID: capability.ID, Profile: capability.ConformanceProfile,
		SuiteSHA256:  capability.ConformanceSuiteSHA256,
		Observations: observations,
	}
	for _, id := range capability.ToolIDs {
		tool, exists := tools[id]
		if !exists {
			return ConformanceEvidence{}, fmt.Errorf("conformance tool %s is unavailable", id)
		}
		evidence.Tools = append(evidence.Tools, ConformanceDependency{ID: id, SHA256: tool.SHA256})
	}
	evidence.Resources = []ConformanceDependency{}
	for _, id := range capability.ResourceIDs {
		resource, exists := resources[id]
		if !exists {
			return ConformanceEvidence{}, fmt.Errorf("conformance resource %s is unavailable", id)
		}
		evidence.Resources = append(evidence.Resources, ConformanceDependency{ID: id, SHA256: resource.SHA256})
	}
	if evidence.Validate() != nil {
		return ConformanceEvidence{}, fmt.Errorf("conformance evidence is invalid")
	}
	return evidence, nil
}

func (evidence ConformanceEvidence) Validate() error {
	if evidence.Schema != ConformanceEvidenceSchema || evidence.Target.Validate() != nil ||
		evidence.Backend != Backend(evidence.Target) ||
		evidence.CapabilityID != CapabilityLocalTranscriptionV1 ||
		evidence.Profile != ConformanceLocalTranscriptionV1 ||
		evidence.SuiteSHA256 != conformanceSuiteDigest(evidence.CapabilityID, evidence.Target) ||
		len(evidence.Tools) == 0 || len(evidence.Tools) > 16 || len(evidence.Resources) > 16 ||
		len(evidence.Observations) == 0 || len(evidence.Observations) > 64 {
		return fmt.Errorf("conformance evidence head is invalid")
	}
	if !validDependencies(evidence.Tools) || !validDependencies(evidence.Resources) {
		return fmt.Errorf("conformance evidence dependency is invalid")
	}
	previous := ""
	for _, observation := range evidence.Observations {
		if !identifier.MatchString(observation.ID) || observation.ID <= previous ||
			!toolchainclosure.ValidDigest(observation.SHA256) {
			return fmt.Errorf("conformance evidence observation is invalid")
		}
		previous = observation.ID
	}
	return nil
}

func validDependencies(values []ConformanceDependency) bool {
	previous := ""
	for _, value := range values {
		if !identifier.MatchString(value.ID) || value.ID <= previous ||
			!toolchainclosure.ValidDigest(value.SHA256) {
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
	data, err := io.ReadAll(io.LimitReader(file, maximumEvidenceBytes+1))
	if err != nil || len(data) == 0 || len(data) > maximumEvidenceBytes {
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

func writeConformanceEvidence(stageRoot string, evidence ConformanceEvidence) (NoticeRecord, error) {
	if evidence.Validate() != nil {
		return NoticeRecord{}, fmt.Errorf("conformance evidence is invalid")
	}
	relative := filepath.ToSlash(filepath.Join(
		"licenses", "whisper", "conformance", evidence.CapabilityID+".json",
	))
	filename := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := atomicfile.WriteJSON(filename, evidence, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	digest, size, err := toolchainclosure.DigestFile(filename)
	if err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{
		ID:   conformanceEvidenceNoticeID(evidence.CapabilityID),
		Path: relative, SHA256: digest, ByteSize: size,
	}, nil
}

func conformanceEvidenceEqual(first, second ConformanceEvidence) bool {
	left, leftErr := json.Marshal(first)
	right, rightErr := json.Marshal(second)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func digestConformanceBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func digestSemanticResult(value whisperResult) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return digestConformanceBytes(encoded)
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (writer *limitedBuffer) Write(data []byte) (int, error) {
	if writer.buffer.Len()+len(data) > writer.limit {
		writer.exceeded = true
		remaining := writer.limit - writer.buffer.Len()
		if remaining > 0 {
			writer.buffer.Write(data[:remaining])
		}
		return len(data), nil
	}
	return writer.buffer.Write(data)
}

func (writer *limitedBuffer) String() string { return writer.buffer.String() }

func conformanceEnvironment() []string {
	return []string{"LC_ALL=C", "LANG=C", "TZ=UTC"}
}

// VerifyCapabilities re-qualifies the closure against the evidence it shipped.
//
// It re-runs the suite rather than trusting the recorded document: evidence
// that is only ever read back proves the file exists, not that the binary still
// behaves. The comparison is byte-exact on the evidence JSON, so a changed
// observation, target or backend is a failure rather than a silent update.
//
// Absence is not failure. A closure without the capability simply has nothing
// to qualify — that is how a target that offers no engine stays typed rather
// than pretending to be a broken one.
func VerifyCapabilities(ctx context.Context, verified Verified) error {
	capability, exists := verified.Capabilities[CapabilityLocalTranscriptionV1]
	if !exists {
		return nil
	}
	if len(capability.Resources) != 1 || len(capability.Resources[0].Files) != 1 {
		return fmt.Errorf("local transcription conformance model closure is invalid")
	}
	recorded, err := readConformanceEvidence(capability.ConformanceEvidence.Path)
	if err != nil {
		recorded, err = readConformanceEvidence(
			filepath.Join(verified.Root, filepath.FromSlash(capability.ConformanceEvidence.Path)),
		)
		if err != nil {
			return fmt.Errorf("read local transcription conformance evidence: %w", err)
		}
	}
	observations, err := Qualify(ctx, capability.Entry.Path, capability.Resources[0].Files[0].Path)
	if err != nil {
		return fmt.Errorf("qualify local transcription: %w", err)
	}
	tools := map[string]ToolRecord{ToolWhisperCLI: {
		ID: ToolWhisperCLI, Path: capability.Entry.Path,
		SHA256: capability.Entry.SHA256, ByteSize: capability.Entry.ByteSize,
	}}
	resources := map[string]ResourceRecord{capability.Resources[0].ID: {
		ID: capability.Resources[0].ID, Kind: capability.Resources[0].Kind,
		Version: capability.Resources[0].Version, Root: capability.Resources[0].Root,
		SHA256: capability.Resources[0].SHA256,
	}}
	rebuilt, err := buildConformanceEvidence(
		verified.Manifest.Target, toCapabilityRecord(capability), tools, resources, observations,
	)
	if err != nil {
		return err
	}
	if !conformanceEvidenceEqual(recorded, rebuilt) {
		return fmt.Errorf("local transcription conformance evidence does not reproduce")
	}
	return nil
}

func toCapabilityRecord(capability Capability) CapabilityRecord {
	record := CapabilityRecord{
		ID: capability.ID, EntryToolID: ToolWhisperCLI,
		ConformanceProfile:          capability.ConformanceProfile,
		ConformanceSuiteSHA256:      capability.ConformanceSuiteSHA256,
		ConformanceEvidenceNoticeID: capability.ConformanceEvidence.ID,
		ClosureSHA256:               capability.ClosureSHA256,
	}
	for _, tool := range capability.Tools {
		record.ToolIDs = append(record.ToolIDs, tool.ID)
	}
	for _, resource := range capability.Resources {
		record.ResourceIDs = append(record.ResourceIDs, resource.ID)
	}
	for _, notice := range capability.Notices {
		record.NoticeIDs = append(record.NoticeIDs, notice.ID)
	}
	return record
}
