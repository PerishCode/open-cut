package mediatoolchain

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/target"
)

const maximumTranscriptionConformanceJSONBytes = 16 << 20

type localTranscriptionConformanceInput struct {
	WhisperPath string
	FFmpegPath  string
	FFprobePath string
	ModelPath   string
	Model       ResourceRecord
}

func localTranscriptionCapabilityRecord(
	baseNotices []NoticeRecord,
	whisperNotice NoticeRecord,
	model ResourceRecord,
) CapabilityRecord {
	noticeIDs := make([]string, 0, len(baseNotices)+2)
	for _, notice := range baseNotices {
		noticeIDs = append(noticeIDs, notice.ID)
	}
	noticeIDs = append(noticeIDs, whisperNotice.ID)
	evidenceID := conformanceEvidenceNoticeID(CapabilityLocalTranscriptionV1)
	noticeIDs = append(noticeIDs, evidenceID)
	slices.Sort(noticeIDs)
	return CapabilityRecord{
		ID: CapabilityLocalTranscriptionV1, EntryToolID: "whisper-cli",
		ToolIDs: []string{"ffmpeg", "ffprobe", "whisper-cli"}, ResourceIDs: []string{model.ID},
		NoticeIDs: noticeIDs, ConformanceProfile: ConformanceLocalTranscriptionV1,
		ConformanceSuiteSHA256:      conformanceSuiteDigest(CapabilityLocalTranscriptionV1),
		ConformanceEvidenceNoticeID: evidenceID,
	}
}

func stageLocalTranscriptionConformanceEvidence(
	ctx context.Context,
	buildTarget target.Target,
	stageRoot string,
	tools []ToolRecord,
	model ResourceRecord,
	capability CapabilityRecord,
) (NoticeRecord, error) {
	toolByID := make(map[string]ToolRecord, len(tools))
	for _, tool := range tools {
		toolByID[tool.ID] = tool
	}
	input, err := localTranscriptionInputFromRecords(stageRoot, toolByID, model)
	if err != nil {
		return NoticeRecord{}, err
	}
	observations, err := qualifyLocalTranscriptionCapability(ctx, input)
	if err != nil {
		return NoticeRecord{}, err
	}
	evidence, err := buildConformanceEvidence(
		buildTarget, capability, toolByID, map[string]ResourceRecord{model.ID: model}, observations,
	)
	if err != nil {
		return NoticeRecord{}, err
	}
	return writeConformanceEvidence(stageRoot, evidence)
}

func localTranscriptionInputFromRecords(
	root string,
	tools map[string]ToolRecord,
	model ResourceRecord,
) (localTranscriptionConformanceInput, error) {
	whisper, whisperOK := tools["whisper-cli"]
	ffmpeg, ffmpegOK := tools["ffmpeg"]
	ffprobe, ffprobeOK := tools["ffprobe"]
	if !whisperOK || !ffmpegOK || !ffprobeOK || model.ID != WhisperConformanceModelID ||
		model.Kind != ResourceKindTranscriptionConformanceModel || len(model.Files) != 1 ||
		model.Files[0].Path != whisperConformanceModelFile {
		return localTranscriptionConformanceInput{}, fmt.Errorf("local transcription conformance closure is unavailable")
	}
	return localTranscriptionConformanceInput{
		WhisperPath: filepath.Join(root, filepath.FromSlash(whisper.Path)),
		FFmpegPath:  filepath.Join(root, filepath.FromSlash(ffmpeg.Path)),
		FFprobePath: filepath.Join(root, filepath.FromSlash(ffprobe.Path)),
		ModelPath:   filepath.Join(root, filepath.FromSlash(model.Root), filepath.FromSlash(model.Files[0].Path)),
		Model:       model,
	}, nil
}

func localTranscriptionInputFromVerified(
	verified Verified,
) (localTranscriptionConformanceInput, error) {
	model := resourceRecord(verified.Manifest.Resources, WhisperConformanceModelID)
	tools := make(map[string]ToolRecord, len(verified.Manifest.Tools))
	for _, tool := range verified.Manifest.Tools {
		tools[tool.ID] = tool
	}
	return localTranscriptionInputFromRecords(verified.Root, tools, model)
}

func verifyLocalTranscriptionConformanceEvidence(
	ctx context.Context,
	verified Verified,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
) error {
	capability, exists := verified.Capabilities[CapabilityLocalTranscriptionV1]
	if !exists {
		return nil
	}
	input, err := localTranscriptionInputFromVerified(verified)
	if err != nil {
		return err
	}
	observations, err := qualifyLocalTranscriptionCapability(ctx, input)
	if err != nil {
		return err
	}
	record := capabilityRecord(verified.Manifest.Capabilities, CapabilityLocalTranscriptionV1)
	expected, err := buildConformanceEvidence(
		verified.Manifest.Target, record, tools, resources, observations,
	)
	if err != nil {
		return err
	}
	actual, err := readConformanceEvidence(filepath.Join(
		verified.Root, filepath.FromSlash(capability.ConformanceEvidence.Path),
	))
	if err != nil || !conformanceEvidenceEqual(actual, expected) {
		return fmt.Errorf("%s conformance evidence mismatch", CapabilityLocalTranscriptionV1)
	}
	return nil
}

func qualifyLocalTranscriptionCapability(
	ctx context.Context,
	input localTranscriptionConformanceInput,
) ([]ConformanceObservation, error) {
	root, err := os.MkdirTemp("", "open-cut-transcription-conformance-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	fixturePath := filepath.Join(root, "source.avi")
	if err := os.WriteFile(fixturePath, conformanceAVI(), 0o600); err != nil {
		return nil, err
	}
	wavPath := filepath.Join(root, "normalized.wav")
	if err := normalizeTranscriptionConformanceAudio(ctx, input.FFmpegPath, root, fixturePath, wavPath); err != nil {
		return nil, err
	}
	wavDigest, _, err := digestFile(wavPath)
	if err != nil {
		return nil, err
	}
	probe, err := runConformanceProbe(ctx, input.FFprobePath, root, wavPath)
	if err != nil || len(probe.Streams) != 1 || probe.Streams[0].CodecName != "pcm_s16le" ||
		probe.Streams[0].CodecType != "audio" || probe.Streams[0].SampleRate != "16000" ||
		probe.Streams[0].Channels != 1 {
		return nil, fmt.Errorf("local transcription normalization is not canonical PCM")
	}
	first, err := runWhisperConformance(ctx, input.WhisperPath, input.ModelPath, root, wavPath, "first")
	if err != nil {
		return nil, err
	}
	second, err := runWhisperConformance(ctx, input.WhisperPath, input.ModelPath, root, wavPath, "second")
	if err != nil {
		return nil, err
	}
	firstDigest := digestConformanceJSON(first)
	if firstDigest == "" || digestConformanceJSON(second) != firstDigest {
		return nil, fmt.Errorf("local transcription semantic output is not stable")
	}
	malformed := filepath.Join(root, "malformed-model.bin")
	if err := os.WriteFile(malformed, []byte("not-a-whisper-model"), 0o600); err != nil {
		return nil, err
	}
	if _, err := runWhisperConformance(ctx, input.WhisperPath, malformed, root, wavPath, "malformed"); err == nil {
		return nil, fmt.Errorf("local transcription accepted a malformed model")
	}
	return []ConformanceObservation{
		{ID: "malformed-model", SHA256: digestConformanceBytes([]byte("rejected"))},
		{ID: "normalized-pcm", SHA256: wavDigest},
		{ID: "semantic-result", SHA256: firstDigest},
	}, nil
}

func normalizeTranscriptionConformanceAudio(
	ctx context.Context,
	ffmpeg, directory, source, output string,
) error {
	rawOutput := output + ".s16le"
	stderr := &limitedConformanceBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: ffmpeg,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-i", source, "-map", "0:a:0",
			"-vn", "-sn", "-dn", "-af",
			"aresample=16000:filter_size=32:phase_shift=10:linear_interp=0:exact_rational=1:async=1:first_pts=0," +
				"aformat=sample_fmts=s16:sample_rates=16000:channel_layouts=mono",
			"-c:a", "pcm_s16le", "-ar", "16000", "-ac", "1", "-map_metadata", "-1",
			"-map_chapters", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-f", "s16le", rawOutput,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
	})
	if err != nil || stderr.exceeded {
		return fmt.Errorf("normalize local transcription conformance audio: %v: %s", err, stderr.String())
	}
	if err := writeTranscriptionConformanceWAV(rawOutput, output); err != nil {
		return err
	}
	return os.Remove(rawOutput)
}

func writeTranscriptionConformanceWAV(rawPath, outputPath string) error {
	info, err := os.Lstat(rawPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size()%2 != 0 || info.Size() > math.MaxUint32-36 {
		return fmt.Errorf("normalized transcription PCM is invalid")
	}
	raw, err := os.Open(rawPath)
	if err != nil {
		return err
	}
	defer raw.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(outputPath)
		}
	}()
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(info.Size()+36))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 16_000)
	binary.LittleEndian.PutUint32(header[28:32], 32_000)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(info.Size()))
	if _, err := output.Write(header); err != nil {
		return err
	}
	if written, err := io.Copy(output, raw); err != nil || written != info.Size() {
		return fmt.Errorf("write transcription conformance WAV")
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

type whisperConformanceDocument struct {
	SystemInfo string `json:"systeminfo"`
	Model      struct {
		Type         string `json:"type"`
		Multilingual bool   `json:"multilingual"`
		Vocab        int64  `json:"vocab"`
		Audio        struct {
			Context int64 `json:"ctx"`
			State   int64 `json:"state"`
			Head    int64 `json:"head"`
			Layer   int64 `json:"layer"`
		} `json:"audio"`
		Text struct {
			Context int64 `json:"ctx"`
			State   int64 `json:"state"`
			Head    int64 `json:"head"`
			Layer   int64 `json:"layer"`
		} `json:"text"`
		Mels  int64 `json:"mels"`
		FType int64 `json:"ftype"`
	} `json:"model"`
	Params struct {
		Model     string `json:"model"`
		Language  string `json:"language"`
		Translate bool   `json:"translate"`
	} `json:"params"`
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []whisperConformanceSegment `json:"transcription"`
}

type whisperConformanceSegment struct {
	Timestamps struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"timestamps"`
	Offsets struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	} `json:"offsets"`
	Text   string `json:"text"`
	Tokens []struct {
		Text       string `json:"text"`
		Timestamps *struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestamps,omitempty"`
		Offsets *struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"offsets,omitempty"`
		ID          int64   `json:"id"`
		Probability float64 `json:"p"`
		DTW         float64 `json:"t_dtw"`
	} `json:"tokens"`
}

type whisperConformanceSemantic struct {
	ModelType string                      `json:"modelType"`
	Language  string                      `json:"language"`
	Segments  []whisperConformanceSegment `json:"segments"`
}

func runWhisperConformance(
	ctx context.Context,
	whisper, model, directory, wav, name string,
) (whisperConformanceSemantic, error) {
	prefix := filepath.Join(directory, name)
	stderr := &limitedConformanceBuffer{limit: 128 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: whisper,
		Args: []string{
			"-m", model, "-f", wav, "-l", "auto", "-ojf", "-of", prefix,
			"-np", "-t", "1", "-p", "1", "-ng", "-nf", "-sow", "-bo", "1", "-bs", "1",
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
	})
	if err != nil || stderr.exceeded {
		return whisperConformanceSemantic{}, fmt.Errorf("run local transcription conformance: %v: %s", err, stderr.String())
	}
	data, err := os.ReadFile(prefix + ".json")
	if err != nil || len(data) == 0 || len(data) > maximumTranscriptionConformanceJSONBytes {
		return whisperConformanceSemantic{}, fmt.Errorf("read local transcription conformance output")
	}
	var document whisperConformanceDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		document.SystemInfo == "" || document.Model.Type != "tiny" || !document.Model.Multilingual ||
		document.Model.Vocab <= 0 || document.Params.Model == "" || document.Params.Language != "auto" ||
		document.Params.Translate || document.Result.Language == "" || len(document.Transcription) > 64 {
		return whisperConformanceSemantic{}, fmt.Errorf("local transcription conformance output is invalid")
	}
	for _, segment := range document.Transcription {
		if segment.Timestamps.From == "" || segment.Timestamps.To == "" ||
			segment.Offsets.From < 0 || segment.Offsets.To < segment.Offsets.From || len(segment.Tokens) > 256 {
			return whisperConformanceSemantic{}, fmt.Errorf("local transcription conformance segment is invalid")
		}
		for _, token := range segment.Tokens {
			if math.IsNaN(token.Probability) || math.IsInf(token.Probability, 0) || token.Probability < 0 ||
				token.Probability > 1 || math.IsNaN(token.DTW) || math.IsInf(token.DTW, 0) {
				return whisperConformanceSemantic{}, fmt.Errorf("local transcription conformance token is invalid")
			}
		}
	}
	return whisperConformanceSemantic{
		ModelType: document.Model.Type, Language: document.Result.Language,
		Segments: document.Transcription,
	}, nil
}
