package renderengine

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

const (
	proxyTimeMapHeaderSize = 16
	proxyTimeMapRecordSize = 16
)

var proxyTimeMapMagic = [8]byte{'O', 'C', 'P', 'M', 'A', 'P', '0', '1'}

type SourceMapSelection struct {
	Ordinal   uint64
	SourcePTS int64
	ProxyPTS  int64
}

type SourceMap struct {
	file   *os.File
	count  uint64
	closed bool
}

type SourceMapCursor struct {
	source       *SourceMap
	initialized  bool
	lastTarget   domain.RationalTime
	lastSelected SourceMapSelection
}

type DecodeTraversalTracker struct {
	limit       uint64
	traversed   uint64
	runStarted  bool
	hasOrdinal  bool
	lastOrdinal uint64
}

func OpenSourceMap(filename string, expectedDigest domain.Digest) (*SourceMap, error) {
	if !cleanAbsoluteRegular(filename) || !validDigest(expectedDigest) {
		return nil, fmt.Errorf("source proxy time map input is invalid")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	failure := func(err error) (*SourceMap, error) {
		file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() < proxyTimeMapHeaderSize+proxyTimeMapRecordSize {
		return failure(fmt.Errorf("source proxy time map size is invalid"))
	}
	digest := sha256.New()
	reader := io.TeeReader(file, digest)
	header := make([]byte, proxyTimeMapHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil || !bytes.Equal(header[:8], proxyTimeMapMagic[:]) {
		return failure(fmt.Errorf("source proxy time map header is invalid"))
	}
	count := binary.BigEndian.Uint64(header[8:])
	if count == 0 || count > rendercontract.MaximumSourceProxyFrames ||
		count > uint64((math.MaxInt64-proxyTimeMapHeaderSize)/proxyTimeMapRecordSize) ||
		info.Size() != proxyTimeMapHeaderSize+int64(count)*proxyTimeMapRecordSize {
		return failure(fmt.Errorf("source proxy time map count is invalid"))
	}
	record := make([]byte, proxyTimeMapRecordSize)
	var previousSource, previousProxy int64
	for ordinal := uint64(0); ordinal < count; ordinal++ {
		if _, err := io.ReadFull(reader, record); err != nil {
			return failure(fmt.Errorf("source proxy time map record is invalid"))
		}
		sourcePTS := int64(binary.BigEndian.Uint64(record[:8]))
		proxyPTS := int64(binary.BigEndian.Uint64(record[8:]))
		if ordinal > 0 && (sourcePTS <= previousSource || proxyPTS <= previousProxy) {
			return failure(fmt.Errorf("source proxy time map order is invalid"))
		}
		previousSource, previousProxy = sourcePTS, proxyPTS
	}
	var trailing [1]byte
	if count, err := reader.Read(trailing[:]); count != 0 || err != io.EOF {
		return failure(fmt.Errorf("source proxy time map has trailing bytes"))
	}
	actualDigest := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if actualDigest != expectedDigest.String() {
		return failure(fmt.Errorf("source proxy time map digest mismatch"))
	}
	return &SourceMap{file: file, count: count}, nil
}

func (source *SourceMap) Close() error {
	if source == nil || source.closed {
		return nil
	}
	source.closed = true
	return source.file.Close()
}

func (source *SourceMap) Count() uint64 {
	if source == nil || source.closed {
		return 0
	}
	return source.count
}

func (source *SourceMap) NewCursor() (*SourceMapCursor, error) {
	if source == nil || source.closed || source.file == nil || source.count == 0 {
		return nil, fmt.Errorf("source proxy time map is unavailable")
	}
	return &SourceMapCursor{source: source}, nil
}

func (cursor *SourceMapCursor) SelectFloor(
	target domain.RationalTime,
	sourceTimeBase domain.RationalTime,
) (SourceMapSelection, error) {
	if cursor == nil || cursor.source == nil || cursor.source.closed || target.Validate() != nil ||
		sourceTimeBase.Validate() != nil || !sourceTimeBase.IsPositive() {
		return SourceMapSelection{}, fmt.Errorf("source map selection input is invalid")
	}
	if cursor.initialized {
		comparison, err := target.Compare(cursor.lastTarget)
		if err != nil {
			return SourceMapSelection{}, err
		}
		if comparison >= 0 {
			selected, err := cursor.selectForward(target, sourceTimeBase)
			if err != nil {
				return SourceMapSelection{}, err
			}
			cursor.lastTarget, cursor.lastSelected = target, selected
			return selected, nil
		}
	}
	selected, err := cursor.selectBinary(target, sourceTimeBase)
	if err != nil {
		return SourceMapSelection{}, err
	}
	cursor.initialized = true
	cursor.lastTarget, cursor.lastSelected = target, selected
	return selected, nil
}

func (cursor *SourceMapCursor) selectForward(
	target domain.RationalTime,
	sourceTimeBase domain.RationalTime,
) (SourceMapSelection, error) {
	selected := cursor.lastSelected
	for selected.Ordinal+1 < cursor.source.count {
		next, err := cursor.source.readRecord(selected.Ordinal + 1)
		if err != nil {
			return SourceMapSelection{}, err
		}
		comparison, err := comparePTSTime(next.SourcePTS, sourceTimeBase, target)
		if err != nil {
			return SourceMapSelection{}, err
		}
		if comparison > 0 {
			break
		}
		selected = next
	}
	return selected, nil
}

func (cursor *SourceMapCursor) selectBinary(
	target domain.RationalTime,
	sourceTimeBase domain.RationalTime,
) (SourceMapSelection, error) {
	first, err := cursor.source.readRecord(0)
	if err != nil {
		return SourceMapSelection{}, err
	}
	comparison, err := comparePTSTime(first.SourcePTS, sourceTimeBase, target)
	if err != nil || comparison >= 0 {
		return first, err
	}
	low, high := uint64(0), cursor.source.count
	for low+1 < high {
		middle := low + (high-low)/2
		record, err := cursor.source.readRecord(middle)
		if err != nil {
			return SourceMapSelection{}, err
		}
		comparison, err := comparePTSTime(record.SourcePTS, sourceTimeBase, target)
		if err != nil {
			return SourceMapSelection{}, err
		}
		if comparison <= 0 {
			low = middle
		} else {
			high = middle
		}
	}
	return cursor.source.readRecord(low)
}

func (source *SourceMap) readRecord(ordinal uint64) (SourceMapSelection, error) {
	if source == nil || source.closed || ordinal >= source.count ||
		ordinal > uint64((math.MaxInt64-proxyTimeMapHeaderSize)/proxyTimeMapRecordSize) {
		return SourceMapSelection{}, fmt.Errorf("source proxy time map ordinal is invalid")
	}
	record := make([]byte, proxyTimeMapRecordSize)
	offset := int64(proxyTimeMapHeaderSize) + int64(ordinal)*proxyTimeMapRecordSize
	if _, err := source.file.ReadAt(record, offset); err != nil {
		return SourceMapSelection{}, fmt.Errorf("read source proxy time map record: %w", err)
	}
	return SourceMapSelection{
		Ordinal: ordinal, SourcePTS: int64(binary.BigEndian.Uint64(record[:8])),
		ProxyPTS: int64(binary.BigEndian.Uint64(record[8:])),
	}, nil
}

func NewDecodeTraversalTracker(limit uint64) (*DecodeTraversalTracker, error) {
	if limit == 0 || limit > MaximumDecodedVideoFrames {
		return nil, fmt.Errorf("decode traversal limit is invalid")
	}
	return &DecodeTraversalTracker{limit: limit}, nil
}

func (tracker *DecodeTraversalTracker) BeginRun() error {
	if tracker == nil || tracker.limit == 0 {
		return fmt.Errorf("decode traversal tracker is invalid")
	}
	tracker.runStarted = true
	tracker.hasOrdinal = false
	tracker.lastOrdinal = 0
	return nil
}

func (tracker *DecodeTraversalTracker) Observe(ordinal uint64) error {
	if tracker == nil || !tracker.runStarted || ordinal >= rendercontract.MaximumSourceProxyFrames {
		return fmt.Errorf("decode traversal observation is invalid")
	}
	var additional uint64
	if !tracker.hasOrdinal {
		additional = ordinal + 1
	} else {
		if ordinal < tracker.lastOrdinal {
			return fmt.Errorf("decode traversal run is not monotonic")
		}
		additional = ordinal - tracker.lastOrdinal
	}
	if additional > tracker.limit-tracker.traversed {
		return ResourceLimitError{Subject: "decoded-video-frames"}
	}
	tracker.traversed += additional
	tracker.lastOrdinal = ordinal
	tracker.hasOrdinal = true
	return nil
}

func (tracker *DecodeTraversalTracker) Traversed() uint64 {
	if tracker == nil {
		return 0
	}
	return tracker.traversed
}

func VideoInstructionSourceTime(
	instruction domain.RenderVideoInstruction,
	outputFrame uint64,
	frameRate domain.RationalTime,
) (domain.RationalTime, bool, error) {
	if frameRate.Validate() != nil || !frameRate.IsPositive() || outputFrame > math.MaxInt64 ||
		frameRate.Value.Value() > math.MaxInt32 ||
		instruction.SourceRange.Start.Validate() != nil || instruction.SourceRange.Duration.Validate() != nil ||
		instruction.TimelineRange.Start.Validate() != nil || instruction.TimelineRange.Duration.Validate() != nil ||
		!instruction.SourceRange.Duration.IsPositive() || !instruction.TimelineRange.Duration.IsPositive() ||
		instruction.TimelineRange.Start.IsNegative() {
		return domain.RationalTime{}, false, fmt.Errorf("video source-time input is invalid")
	}
	equalDuration, err := instruction.SourceRange.Duration.Compare(instruction.TimelineRange.Duration)
	if err != nil || equalDuration != 0 {
		return domain.RationalTime{}, false, fmt.Errorf("video source-time ranges are invalid")
	}
	numerator, overflow := multiplyUint64(outputFrame, uint64(frameRate.Scale))
	if overflow || numerator > math.MaxInt64 {
		return domain.RationalTime{}, false, ResourceLimitError{Subject: "output-frame-time"}
	}
	outputTime, err := domain.NewRationalTime(int64(numerator), int32(frameRate.Value.Value()))
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	end, err := instruction.TimelineRange.End()
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	startComparison, err := outputTime.Compare(instruction.TimelineRange.Start)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	endComparison, err := outputTime.Compare(end)
	if err != nil || startComparison < 0 || endComparison >= 0 {
		return domain.RationalTime{}, false, err
	}
	negativeTimelineStart, err := domain.NewRationalTime(
		-instruction.TimelineRange.Start.Value.Value(), instruction.TimelineRange.Start.Scale,
	)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	offset, err := outputTime.Add(negativeTimelineStart)
	if err != nil {
		return domain.RationalTime{}, false, err
	}
	sourceTime, err := instruction.SourceRange.Start.Add(offset)
	return sourceTime, err == nil, err
}

func comparePTSTime(pts int64, timeBase, target domain.RationalTime) (int, error) {
	if timeBase.Validate() != nil || !timeBase.IsPositive() || target.Validate() != nil {
		return 0, fmt.Errorf("source PTS comparison input is invalid")
	}
	left := new(big.Int).Mul(big.NewInt(pts), big.NewInt(timeBase.Value.Value()))
	left.Mul(left, big.NewInt(int64(target.Scale)))
	right := new(big.Int).Mul(big.NewInt(target.Value.Value()), big.NewInt(int64(timeBase.Scale)))
	return left.Cmp(right), nil
}
