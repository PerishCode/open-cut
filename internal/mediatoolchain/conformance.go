package mediatoolchain

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
)

const maximumConformanceOutputBytes = 256 << 10

type conformanceProbeDocument struct {
	Streams []struct {
		CodecName  string `json:"codec_name"`
		CodecType  string `json:"codec_type"`
		Width      uint32 `json:"width"`
		Height     uint32 `json:"height"`
		SampleRate string `json:"sample_rate"`
		Channels   uint16 `json:"channels"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func VerifyCapabilities(ctx context.Context, verified Verified) error {
	if err := VerifyRendererRelink(ctx, verified); err != nil {
		return err
	}
	if err := VerifyBaseCapabilities(ctx, verified); err != nil {
		return err
	}
	return VerifyRendererCapabilities(ctx, verified)
}

// VerifyBaseCapabilities replays the probe, frame, proxy, and render-input
// qualification suite and requires it to reproduce the contained evidence.
// It is exported separately so an app-owned packaging gate can measure this
// semantically closed stage without duplicating verification logic.
func VerifyBaseCapabilities(ctx context.Context, verified Verified) error {
	probeCapability, exists := verified.Capabilities[CapabilityProbeV1]
	if !exists || probeCapability.Entry.Path == "" {
		return fmt.Errorf("probe-v1 conformance capability is unavailable")
	}
	frameCapability, exists := verified.Capabilities[CapabilityFrameRGBV1]
	if !exists || frameCapability.Entry.Path == "" {
		return fmt.Errorf("frame-rgb24-v1 conformance capability is unavailable")
	}
	proxyCapability, exists := verified.Capabilities[CapabilitySourceProxyV1]
	if !exists || proxyCapability.Entry.Path == "" {
		return fmt.Errorf("source-proxy-webm-vp9-opus-v1 conformance capability is unavailable")
	}
	renderInputCapability, exists := verified.Capabilities[CapabilityRenderInputV1]
	if !exists || renderInputCapability.Entry.Path == "" {
		return fmt.Errorf("render-input-matroska-ffv1-pcm-v1 conformance capability is unavailable")
	}
	observations, err := qualifyBaseCapabilities(
		ctx, probeCapability.Entry.Path, frameCapability.Entry.Path,
		proxyCapability.Entry.Path, renderInputCapability.Entry.Path,
	)
	if err != nil {
		return err
	}
	tools, resources := verificationDependencies(verified)
	for _, capabilityID := range []string{
		CapabilityProbeV1, CapabilityFrameRGBV1, CapabilitySourceProxyV1, CapabilityRenderInputV1,
	} {
		capability := verified.Capabilities[capabilityID]
		record := capabilityRecord(verified.Manifest.Capabilities, capabilityID)
		expected, buildErr := buildConformanceEvidence(
			verified.Manifest.Target, record, tools, resources, observations[capabilityID],
		)
		if buildErr != nil {
			return buildErr
		}
		actual, readErr := readConformanceEvidence(filepath.Join(
			verified.Root, filepath.FromSlash(capability.ConformanceEvidence.Path),
		))
		if readErr != nil || !conformanceEvidenceEqual(actual, expected) {
			return fmt.Errorf("%s conformance evidence mismatch", capabilityID)
		}
	}
	return nil
}

// VerifyRendererCapabilities replays every declared renderer matrix and
// requires its observations to reproduce the contained evidence.
func VerifyRendererCapabilities(ctx context.Context, verified Verified) error {
	tools, resources := verificationDependencies(verified)
	for _, capabilityID := range []string{
		CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1,
	} {
		if _, exists := verified.Capabilities[capabilityID]; exists {
			if err := verifyRendererConformanceEvidence(ctx, verified, tools, resources, capabilityID); err != nil {
				return err
			}
		}
	}
	return nil
}

func verificationDependencies(verified Verified) (map[string]ToolRecord, map[string]ResourceRecord) {
	tools := make(map[string]ToolRecord, len(verified.Manifest.Tools))
	for _, tool := range verified.Manifest.Tools {
		tools[tool.ID] = tool
	}
	resources := make(map[string]ResourceRecord, len(verified.Manifest.Resources))
	for _, resource := range verified.Manifest.Resources {
		resources[resource.ID] = resource
	}
	return tools, resources
}

func qualifyBaseCapabilities(
	ctx context.Context,
	probePath string,
	frameDecoderPath string,
	proxyEncoderPath string,
	renderInputEncoderPath string,
) (map[string][]ConformanceObservation, error) {
	root, err := os.MkdirTemp("", "open-cut-media-conformance-")
	if err != nil {
		return nil, fmt.Errorf("create media conformance root: %w", err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	validPath := filepath.Join(root, "probe-v1.avi")
	if err := os.WriteFile(validPath, conformanceAVI(), 0o600); err != nil {
		return nil, err
	}
	document, err := runConformanceProbe(ctx, probePath, root, validPath)
	if err != nil {
		return nil, fmt.Errorf("probe-v1 rejected the canonical fixture: %w", err)
	}
	if err := validateConformanceDocument(document); err != nil {
		return nil, err
	}
	frameDigest, err := runConformanceFrameDecode(ctx, frameDecoderPath, root, validPath)
	if err != nil {
		return nil, fmt.Errorf("frame-rgb24-v1 rejected the canonical fixture: %w", err)
	}
	proxyPath := filepath.Join(root, "source-proxy-v1.webm")
	if err := runConformanceProxyEncode(ctx, proxyEncoderPath, root, validPath, proxyPath); err != nil {
		return nil, fmt.Errorf("source-proxy-webm-vp9-opus-v1 rejected the canonical fixture: %w", err)
	}
	proxyDocument, err := runConformanceProbe(ctx, probePath, root, proxyPath)
	if err != nil || validateProxyConformanceDocument(proxyDocument) != nil {
		return nil, fmt.Errorf("source proxy output failed conformance")
	}
	proxyDigest, _, err := digestFile(proxyPath)
	if err != nil {
		return nil, err
	}
	repeatedProxyPath := filepath.Join(root, "source-proxy-v1-repeated.webm")
	if err := runConformanceProxyEncode(ctx, proxyEncoderPath, root, validPath, repeatedProxyPath); err != nil {
		return nil, fmt.Errorf("source proxy repeat failed conformance: %w", err)
	}
	repeatedProxyDigest, _, err := digestFile(repeatedProxyPath)
	if err != nil || repeatedProxyDigest != proxyDigest {
		first, leftWindow, rightWindow := firstConformanceDifference(proxyPath, repeatedProxyPath)
		return nil, fmt.Errorf(
			"source proxy output is not byte stable: %s != %s at %d (%x != %x)",
			proxyDigest, repeatedProxyDigest, first, leftWindow, rightWindow,
		)
	}
	renderInputPath := filepath.Join(root, "render-input-v1.mkv")
	if err := runConformanceRenderInputEncode(
		ctx, renderInputEncoderPath, root, validPath, renderInputPath,
	); err != nil {
		return nil, fmt.Errorf("render-input-matroska-ffv1-pcm-v1 rejected the canonical fixture: %w", err)
	}
	renderInputDocument, err := runConformanceProbe(ctx, probePath, root, renderInputPath)
	if err != nil || validateRenderInputConformanceDocument(renderInputDocument) != nil {
		return nil, fmt.Errorf("render-input output failed conformance")
	}
	renderInputDigest, _, err := digestFile(renderInputPath)
	if err != nil {
		return nil, err
	}
	repeatedRenderInputPath := filepath.Join(root, "render-input-v1-repeated.mkv")
	if err := runConformanceRenderInputEncode(
		ctx, renderInputEncoderPath, root, validPath, repeatedRenderInputPath,
	); err != nil {
		return nil, fmt.Errorf("render-input repeat failed conformance: %w", err)
	}
	repeatedRenderInputDigest, _, err := digestFile(repeatedRenderInputPath)
	if err != nil || repeatedRenderInputDigest != renderInputDigest {
		first, leftWindow, rightWindow := firstConformanceDifference(renderInputPath, repeatedRenderInputPath)
		return nil, fmt.Errorf(
			"render-input output is not byte stable: %s != %s at %d (%x != %x)",
			renderInputDigest, repeatedRenderInputDigest, first, leftWindow, rightWindow,
		)
	}
	malformedPath := filepath.Join(root, "malformed.avi")
	if err := os.WriteFile(malformedPath, []byte("RIFF\x10\x00\x00\x00AVI LIST"), 0o600); err != nil {
		return nil, err
	}
	if _, err := runConformanceProbe(ctx, probePath, root, malformedPath); err == nil {
		return nil, fmt.Errorf("probe-v1 accepted the malformed fixture")
	}
	return map[string][]ConformanceObservation{
		CapabilityProbeV1: {
			{ID: "probe-document", SHA256: digestConformanceJSON(document)},
			{ID: "truncated-riff", SHA256: digestConformanceBytes([]byte("rejected"))},
		},
		CapabilityFrameRGBV1: {
			{ID: "rgb24-frame", SHA256: frameDigest},
		},
		CapabilitySourceProxyV1: {
			{ID: "media-facts", SHA256: digestConformanceJSON(proxyDocument)},
			{ID: "webm-bytes", SHA256: proxyDigest},
		},
		CapabilityRenderInputV1: {
			{ID: "media-facts", SHA256: digestConformanceJSON(renderInputDocument)},
			{ID: "matroska-bytes", SHA256: renderInputDigest},
		},
	}, nil
}

func firstConformanceDifference(leftPath, rightPath string) (int, []byte, []byte) {
	left, leftErr := os.ReadFile(leftPath)
	right, rightErr := os.ReadFile(rightPath)
	if leftErr != nil || rightErr != nil {
		return -1, nil, nil
	}
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	if index == limit && len(left) == len(right) {
		return -1, nil, nil
	}
	end := index + 32
	if end > len(left) {
		end = len(left)
	}
	rightEnd := index + 32
	if rightEnd > len(right) {
		rightEnd = len(right)
	}
	return index, left[index:end], right[index:rightEnd]
}

func runConformanceProxyEncode(ctx context.Context, executable, directory, fixture, output string) error {
	stderr := &limitedConformanceBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd",
			"-i", fixture, "-map", "0:v:0", "-map", "0:a:0", "-map_metadata", "-1", "-map_chapters", "-1",
			"-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p", "-deadline", "good", "-cpu-used", "4",
			"-threads", "1", "-row-mt", "0", "-tile-columns", "0", "-frame-parallel", "0",
			"-b:v", "0", "-crf", "32", "-g", "4", "-flags:v", "+bitexact", "-fps_mode", "passthrough",
			"-c:a", "libopus", "-ar", "48000", "-ac", "2", "-b:a", "128k", "-vbr", "off",
			"-compression_level", "10", "-frame_duration", "20", "-flags:a", "+bitexact", "-fflags", "+bitexact",
			"-f", "webm", output,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stderr.exceeded {
		if executionContext.Err() != nil {
			return executionContext.Err()
		}
		return fmt.Errorf("ffmpeg proxy encode failed (%v): %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := os.Stat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("ffmpeg proxy encode produced no regular output")
	}
	return nil
}

func validateProxyConformanceDocument(document conformanceProbeDocument) error {
	aliases := strings.Split(document.Format.FormatName, ",")
	if (!containsString(aliases, "webm") && !containsString(aliases, "matroska")) || len(document.Streams) != 2 {
		return fmt.Errorf("proxy container or stream count is invalid")
	}
	video, audio := false, false
	for _, stream := range document.Streams {
		switch stream.CodecType {
		case "video":
			video = stream.CodecName == "vp9" && stream.Width == 16 && stream.Height == 16
		case "audio":
			audio = stream.CodecName == "opus" && stream.SampleRate == "48000" && stream.Channels == 2
		}
	}
	if !video || !audio {
		return fmt.Errorf("proxy codec facts are invalid")
	}
	return nil
}

func runConformanceRenderInputEncode(
	ctx context.Context,
	executable string,
	directory string,
	fixture string,
	output string,
) error {
	stderr := &limitedConformanceBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-i", fixture,
			"-filter_complex",
			"[0:v:0]format=yuv420p,setsar=1,setparams=range=limited:color_primaries=bt709:color_trc=bt709:colorspace=bt709[v];" +
				"[0:a:0]aresample=48000:dither_method=none:osf=s16,aformat=sample_fmts=s16:channel_layouts=stereo[a]",
			"-map", "[v]", "-map", "[a]", "-map_metadata", "-1", "-map_chapters", "-1",
			"-c:v", "ffv1", "-level", "3", "-coder", "1", "-context", "1", "-g", "1",
			"-slicecrc", "1", "-threads", "1", "-pix_fmt", "yuv420p", "-flags:v", "+bitexact",
			"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv",
			"-c:a", "pcm_s16le", "-ar", "48000", "-ac", "2", "-flags:a", "+bitexact",
			"-fflags", "+bitexact", "-f", "matroska", output,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stderr.exceeded {
		if executionContext.Err() != nil {
			return executionContext.Err()
		}
		return fmt.Errorf("ffmpeg render-input encode failed (%v): %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := os.Stat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("ffmpeg render-input encode produced no regular output")
	}
	return nil
}

func validateRenderInputConformanceDocument(document conformanceProbeDocument) error {
	aliases := strings.Split(document.Format.FormatName, ",")
	if !containsString(aliases, "matroska") || len(document.Streams) != 2 {
		return fmt.Errorf("render-input container or stream count is invalid")
	}
	video, audio := false, false
	for _, stream := range document.Streams {
		switch stream.CodecType {
		case "video":
			video = stream.CodecName == "ffv1" && stream.Width == 16 && stream.Height == 16
		case "audio":
			audio = stream.CodecName == "pcm_s16le" && stream.SampleRate == "48000" && stream.Channels == 2
		}
	}
	if !video || !audio {
		return fmt.Errorf("render-input codec facts are invalid")
	}
	return nil
}

// WriteCanonicalConformanceFixture materializes the same deterministic media
// sample used by payload capability checks for cross-layer harnesses.
func WriteCanonicalConformanceFixture(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("media conformance fixture path is invalid")
	}
	return os.WriteFile(path, conformanceAVI(), 0o600)
}

func runConformanceFrameDecode(ctx context.Context, executable, directory, fixture string) (string, error) {
	stdout := &limitedConformanceBuffer{limit: maximumConformanceOutputBytes}
	stderr := &limitedConformanceBuffer{limit: 16 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-protocol_whitelist", "file,pipe,fd",
			"-i", fixture, "-map", "0:v:0", "-vf", "scale=8:8,format=rgb24",
			"-frames:v", "1", "-f", "rawvideo", "pipe:1",
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		if executionContext.Err() != nil {
			return "", executionContext.Err()
		}
		return "", fmt.Errorf("ffmpeg failed: %s", strings.TrimSpace(stderr.String()))
	}
	const expectedBytes = 8 * 8 * 3
	if stdout.Len() != expectedBytes {
		return "", fmt.Errorf("ffmpeg returned %d bytes, expected %d", stdout.Len(), expectedBytes)
	}
	first := stdout.Bytes()[0]
	nonUniform := false
	for _, value := range stdout.Bytes()[1:] {
		if value != first {
			nonUniform = true
			break
		}
	}
	if !nonUniform {
		return "", fmt.Errorf("ffmpeg returned a uniform canonical frame")
	}
	return digestConformanceBytes(stdout.Bytes()), nil
}

func runConformanceProbe(
	ctx context.Context,
	executable string,
	directory string,
	fixture string,
) (conformanceProbeDocument, error) {
	stdout := &limitedConformanceBuffer{limit: maximumConformanceOutputBytes}
	stderr := &limitedConformanceBuffer{limit: 16 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-show_entries", "format=format_name,duration:stream=codec_name,codec_type,width,height,sample_rate,channels",
			"-of", "json=compact=1", fixture,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		if executionContext.Err() != nil {
			return conformanceProbeDocument{}, executionContext.Err()
		}
		return conformanceProbeDocument{}, fmt.Errorf("ffprobe failed: %s", strings.TrimSpace(stderr.String()))
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var document conformanceProbeDocument
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return conformanceProbeDocument{}, fmt.Errorf("ffprobe returned invalid JSON: %s", strings.TrimSpace(stdout.String()))
	}
	return document, nil
}

func validateConformanceDocument(document conformanceProbeDocument) error {
	aliases := strings.Split(document.Format.FormatName, ",")
	if !containsString(aliases, "avi") || len(document.Streams) != 2 {
		return fmt.Errorf("probe-v1 did not preserve the canonical container and stream inventory")
	}
	duration, err := strconv.ParseFloat(document.Format.Duration, 64)
	if err != nil || duration < 0.99 || duration > 1.01 {
		return fmt.Errorf("probe-v1 returned an invalid canonical duration")
	}
	video, audio := false, false
	for _, stream := range document.Streams {
		switch stream.CodecType {
		case "video":
			video = stream.CodecName == "rawvideo" && stream.Width == 16 && stream.Height == 16
		case "audio":
			audio = stream.CodecName == "pcm_s16le" && stream.SampleRate == "8000" && stream.Channels == 2
		}
	}
	if !video || !audio {
		return fmt.Errorf("probe-v1 did not preserve canonical video and stereo audio facts")
	}
	return nil
}

func conformanceAVI() []byte {
	const (
		width      = 16
		height     = 16
		frames     = 2
		frameRate  = 2
		sampleRate = 8000
		channels   = 2
		bits       = 16
	)
	rowBytes := ((width*3 + 3) / 4) * 4
	frameBytes := rowBytes * height
	blockAlign := channels * bits / 8
	byteRate := sampleRate * blockAlign

	mainHeader := new(bytes.Buffer)
	writeU32(mainHeader, 1_000_000/frameRate, uint32(byteRate+frameBytes*frameRate), 0, 0, frames, 0, 2, frameBytes, width, height)
	writeU32(mainHeader, 0, 0, 0, 0)

	videoHeader := new(bytes.Buffer)
	videoHeader.WriteString("vids")
	videoHeader.WriteString("DIB ")
	writeU32(videoHeader, 0)
	writeU16(videoHeader, 0, 0)
	writeU32(videoHeader, 0, 1, frameRate, 0, frames, frameBytes, ^uint32(0), 0)
	writeI16(videoHeader, 0, 0, width, height)
	videoFormat := new(bytes.Buffer)
	writeU32(videoFormat, 40, width, height)
	writeU16(videoFormat, 1, 24)
	writeU32(videoFormat, 0, frameBytes, 0, 0, 0, 0)
	videoList := listChunk("strl", append(riffChunk("strh", videoHeader.Bytes()), riffChunk("strf", videoFormat.Bytes())...))

	audioHeader := new(bytes.Buffer)
	audioHeader.WriteString("auds")
	writeU32(audioHeader, 0, 0)
	writeU16(audioHeader, 0, 0)
	writeU32(audioHeader, 0, blockAlign, byteRate, 0, sampleRate, blockAlign*1024, ^uint32(0), blockAlign)
	writeI16(audioHeader, 0, 0, 0, 0)
	audioFormat := new(bytes.Buffer)
	writeU16(audioFormat, 1, channels)
	writeU32(audioFormat, sampleRate, byteRate)
	writeU16(audioFormat, blockAlign, bits)
	audioList := listChunk("strl", append(riffChunk("strh", audioHeader.Bytes()), riffChunk("strf", audioFormat.Bytes())...))

	hdrl := append(riffChunk("avih", mainHeader.Bytes()), videoList...)
	hdrl = append(hdrl, audioList...)
	movie := make([]byte, 0, frames*(frameBytes+8)+byteRate+8)
	for frame := 0; frame < frames; frame++ {
		movie = append(movie, riffChunk("00db", conformanceVideoFrame(width, height, rowBytes, frame))...)
	}
	movie = append(movie, riffChunk("01wb", conformanceAudio(sampleRate, channels))...)
	body := append([]byte("AVI "), listChunk("hdrl", hdrl)...)
	body = append(body, listChunk("movi", movie)...)
	return riffChunk("RIFF", body)
}

func conformanceVideoFrame(width, height, rowBytes, frame int) []byte {
	result := make([]byte, rowBytes*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*rowBytes + x*3
			if frame == 0 {
				result[offset], result[offset+1], result[offset+2] = byte(x*8), byte(y*8), 0xff
			} else {
				result[offset], result[offset+1], result[offset+2] = 0xff, byte(x*8), byte(y*8)
			}
		}
	}
	return result
}

func conformanceAudio(sampleRate, channels int) []byte {
	result := new(bytes.Buffer)
	for sample := 0; sample < sampleRate; sample++ {
		phase := sample % 80
		value := int16(12_000)
		if phase >= 40 {
			value = -value
		}
		for channel := 0; channel < channels; channel++ {
			_ = binary.Write(result, binary.LittleEndian, value)
		}
	}
	return result.Bytes()
}

func riffChunk(id string, payload []byte) []byte {
	result := new(bytes.Buffer)
	result.WriteString(id)
	writeU32(result, len(payload))
	result.Write(payload)
	if len(payload)%2 != 0 {
		result.WriteByte(0)
	}
	return result.Bytes()
}

func listChunk(kind string, payload []byte) []byte {
	return riffChunk("LIST", append([]byte(kind), payload...))
}

func writeU16(writer io.Writer, values ...any) {
	for _, value := range values {
		_ = binary.Write(writer, binary.LittleEndian, uint16(toInt(value)))
	}
}

func writeI16(writer io.Writer, values ...any) {
	for _, value := range values {
		_ = binary.Write(writer, binary.LittleEndian, int16(toInt(value)))
	}
}

func writeU32(writer io.Writer, values ...any) {
	for _, value := range values {
		_ = binary.Write(writer, binary.LittleEndian, uint32(toInt(value)))
	}
}

func toInt(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case uint32:
		return int64(typed)
	default:
		panic("unsupported conformance integer")
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func conformanceEnvironment() []string {
	allowed := map[string]struct{}{
		"LANG": {}, "LC_ALL": {}, "SYSTEMROOT": {}, "WINDIR": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {},
	}
	result := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if _, ok := allowed[name]; found && ok {
			result = append(result, entry)
		}
	}
	return result
}

type limitedConformanceBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *limitedConformanceBuffer) Write(value []byte) (int, error) {
	if buffer.exceeded {
		return len(value), nil
	}
	remaining := buffer.limit - buffer.Len()
	if len(value) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(value[:remaining])
		}
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}
