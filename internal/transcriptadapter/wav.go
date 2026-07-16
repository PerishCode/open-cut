package transcriptadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/PerishCode/open-cut/product/domain"
)

func InspectWAV(
	path string,
	sourceStart domain.RationalTime,
	channelPolicy string,
) (domain.TranscriptNormalizationProof, error) {
	info, err := os.Lstat(path)
	maximum := int64(domain.MaximumTranscriptSamples*2 + 1<<20)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 44 || info.Size() > maximum {
		return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.TranscriptNormalizationProof{}, err
	}
	defer file.Close()
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil || !bytes.Equal(header[:4], []byte("RIFF")) ||
		!bytes.Equal(header[8:12], []byte("WAVE")) || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != info.Size() {
		return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
	}
	var dataOffset int64
	var dataSize uint32
	seenFormat, seenData := false, false
	for offset := int64(12); offset+8 <= info.Size(); {
		chunk := make([]byte, 8)
		if _, err := file.ReadAt(chunk, offset); err != nil {
			return domain.TranscriptNormalizationProof{}, err
		}
		size := binary.LittleEndian.Uint32(chunk[4:8])
		payload := offset + 8
		end := payload + int64(size)
		if end < payload || end > info.Size() {
			return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if seenFormat || size < 16 || size > 64 {
				return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
			}
			format := make([]byte, size)
			if _, err := file.ReadAt(format, payload); err != nil ||
				binary.LittleEndian.Uint16(format[0:2]) != 1 || binary.LittleEndian.Uint16(format[2:4]) != 1 ||
				binary.LittleEndian.Uint32(format[4:8]) != domain.TranscriptSampleRate ||
				binary.LittleEndian.Uint32(format[8:12]) != domain.TranscriptSampleRate*2 ||
				binary.LittleEndian.Uint16(format[12:14]) != 2 || binary.LittleEndian.Uint16(format[14:16]) != 16 {
				return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
			}
			seenFormat = true
		case "data":
			if seenData || size == 0 || size%2 != 0 {
				return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
			}
			dataOffset, dataSize, seenData = payload, size, true
		}
		offset = end + int64(size%2)
	}
	if !seenFormat || !seenData || uint64(dataSize/2) > domain.MaximumTranscriptSamples {
		return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
	}
	hash := sha256.New()
	if written, err := io.Copy(hash, io.NewSectionReader(file, dataOffset, int64(dataSize))); err != nil || written != int64(dataSize) {
		return domain.TranscriptNormalizationProof{}, fmt.Errorf("hash normalized transcript PCM: %w", err)
	}
	samples, _ := domain.NewUInt64(uint64(dataSize / 2))
	byteSize, _ := domain.NewUInt64(uint64(dataSize))
	proof := domain.TranscriptNormalizationProof{
		SourceStartTime: sourceStart, SampleRate: domain.TranscriptSampleRate, Channels: 1,
		SampleFormat: "s16le", SampleCount: samples, PCMByteSize: byteSize,
		PCMDigest:     domain.Digest("sha256:" + hex.EncodeToString(hash.Sum(nil))),
		ChannelPolicy: channelPolicy, TimingPolicy: "audio-frame-pts-gap-fill-v1",
	}
	if proof.Validate() != nil {
		return domain.TranscriptNormalizationProof{}, domain.ErrInvalidTranscript
	}
	return proof, nil
}
