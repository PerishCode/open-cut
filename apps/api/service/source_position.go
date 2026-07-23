package service

import (
	"context"
	"math"
	"math/big"
	"os"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type SourcePositionOperation string

const (
	SourcePositionSettle   SourcePositionOperation = "settle"
	SourcePositionPrevious SourcePositionOperation = "previous"
	SourcePositionNext     SourcePositionOperation = "next"
)

type SourcePositionBoundary string

const (
	SourcePositionVideoPresentation SourcePositionBoundary = "video-presentation"
	SourcePositionAudioSample       SourcePositionBoundary = "audio-sample"
	SourcePositionCoverageEnd       SourcePositionBoundary = "coverage-end"
)

type SourcePositionRequest struct {
	ResourceID domain.ResourceID       `json:"resourceId" format:"uuid"`
	Operation  SourcePositionOperation `json:"operation" enum:"settle,previous,next"`
	Target     domain.RationalTime     `json:"target"`
}

type SourcePositionResult struct {
	ResourceID    domain.ResourceID       `json:"resourceId" format:"uuid"`
	ProjectID     domain.ProjectID        `json:"projectId" format:"uuid"`
	AssetID       domain.AssetID          `json:"assetId" format:"uuid"`
	AssetRevision domain.Revision         `json:"assetRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Fingerprint   domain.Digest           `json:"fingerprint" format:"sha256-digest"`
	VideoStreamID *domain.SourceStreamID  `json:"videoStreamId,omitempty"`
	AudioStreamID *domain.SourceStreamID  `json:"audioStreamId,omitempty"`
	Operation     SourcePositionOperation `json:"operation" enum:"settle,previous,next"`
	RequestedTime domain.RationalTime     `json:"requestedTime"`
	SourceTime    domain.RationalTime     `json:"sourceTime"`
	ProxyTime     domain.RationalTime     `json:"proxyTime"`
	Boundary      SourcePositionBoundary  `json:"boundary" enum:"video-presentation,audio-sample,coverage-end"`
	AtStart       bool                    `json:"atStart"`
	AtEnd         bool                    `json:"atEnd"`
}

func (service *MediaLeaseService) ResolveSourcePosition(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	request SourcePositionRequest,
) (SourcePositionResult, error) {
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil {
		return SourcePositionResult{}, err
	}
	if projectID.IsZero() || assetID.IsZero() || request.ResourceID.IsZero() ||
		request.Target.Validate() != nil || !validSourcePositionOperation(request.Operation) {
		return SourcePositionResult{}, ErrMediaLeaseInvalid
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	record, found := service.sourcePositionLeaseLocked(request.ResourceID)
	service.mu.Unlock()
	if !found || record.projectID != projectID || record.assetID != assetID ||
		!record.binding.matches(binding) {
		return SourcePositionResult{}, ErrMediaLeaseInvalid
	}
	if !now.Before(record.expiresAt) {
		return SourcePositionResult{}, ErrMediaLeaseExpired
	}
	result := sourcePositionResultForRecord(record, request)
	if record.manifest.Video != nil {
		file, manifest, openErr := service.opener.OpenSourceProxyVideoTimeMap(
			ctx, record.projectID, record.assetID, record.artifactID,
		)
		if openErr != nil {
			return SourcePositionResult{}, openErr
		}
		defer file.Close()
		if !matchingSourcePositionManifest(record.manifest, manifest) {
			return SourcePositionResult{}, ErrMediaLeaseInvalid
		}
		position, resolveErr := ResolveVideoSourcePosition(file, *manifest.Video, request)
		if resolveErr != nil {
			return SourcePositionResult{}, resolveErr
		}
		result.SourceTime = position.Source
		result.ProxyTime = position.Proxy
		result.Boundary = position.Boundary
		result.AtStart = position.AtStart
		result.AtEnd = position.AtEnd
		return result, nil
	}
	if record.manifest.Audio == nil {
		return SourcePositionResult{}, ErrMediaLeaseInvalid
	}
	position, err := ResolveAudioSourcePosition(*record.manifest.Audio, request)
	if err != nil {
		return SourcePositionResult{}, err
	}
	result.SourceTime = position.Source
	result.ProxyTime = position.Proxy
	result.Boundary = position.Boundary
	result.AtStart = position.AtStart
	result.AtEnd = position.AtEnd
	return result, nil
}

func validSourcePositionOperation(operation SourcePositionOperation) bool {
	return operation == SourcePositionSettle || operation == SourcePositionPrevious || operation == SourcePositionNext
}

func (service *MediaLeaseService) sourcePositionLeaseLocked(resourceID domain.ResourceID) (mediaLeaseRecord, bool) {
	for _, record := range service.leases {
		if record.resourceID == resourceID {
			return record, true
		}
	}
	return mediaLeaseRecord{}, false
}

func sourcePositionResultForRecord(
	record mediaLeaseRecord,
	request SourcePositionRequest,
) SourcePositionResult {
	result := SourcePositionResult{
		ResourceID: request.ResourceID, ProjectID: record.projectID, AssetID: record.assetID,
		AssetRevision: record.assetRevision, Fingerprint: record.manifest.Fingerprint,
		Operation: request.Operation, RequestedTime: request.Target,
	}
	if record.manifest.Video != nil {
		id := record.manifest.Video.Source.ID
		result.VideoStreamID = &id
	}
	if record.manifest.Audio != nil {
		id := record.manifest.Audio.Source.ID
		result.AudioStreamID = &id
	}
	return result
}

func matchingSourcePositionManifest(
	expected application.SourceProxyArtifactManifest,
	actual application.SourceProxyArtifactManifest,
) bool {
	if expected.AssetID != actual.AssetID || expected.Fingerprint != actual.Fingerprint ||
		expected.SourceEpoch != actual.SourceEpoch ||
		(expected.Video == nil) != (actual.Video == nil) || (expected.Audio == nil) != (actual.Audio == nil) {
		return false
	}
	if expected.Video != nil && (expected.Video.Source.ID != actual.Video.Source.ID ||
		expected.Video.TimeMap.SHA256 != actual.Video.TimeMap.SHA256 ||
		expected.Video.FrameCount != actual.Video.FrameCount) {
		return false
	}
	if expected.Audio != nil && (expected.Audio.Source.ID != actual.Audio.Source.ID ||
		expected.Audio.DecodedSampleCount != actual.Audio.DecodedSampleCount) {
		return false
	}
	return true
}

type ResolvedSourcePosition struct {
	Source   domain.RationalTime
	Proxy    domain.RationalTime
	Boundary SourcePositionBoundary
	AtStart  bool
	AtEnd    bool
}

func ResolveVideoSourcePosition(
	file *os.File,
	track application.SourceProxyVideoTrack,
	request SourcePositionRequest,
) (ResolvedSourcePosition, error) {
	count := track.FrameCount.Value()
	upper, err := firstVideoPointAfter(file, track, request.Target)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	index := uint64(0)
	switch request.Operation {
	case SourcePositionSettle:
		if upper > 0 {
			index = upper - 1
		}
	case SourcePositionPrevious:
		if upper > 0 {
			index = upper - 1
			point, readErr := application.ReadSourceProxyTimeMapPointAt(file, count, index)
			if readErr != nil {
				return ResolvedSourcePosition{}, readErr
			}
			comparison, compareErr := compareTicksToTime(point.SourcePTS, track.Source.Descriptor.TimeBase, request.Target)
			if compareErr != nil {
				return ResolvedSourcePosition{}, compareErr
			}
			if comparison == 0 && index > 0 {
				index--
			}
		}
	case SourcePositionNext:
		if upper < count {
			index = upper
		} else {
			last, readErr := videoPointPosition(file, track, count-1)
			if readErr != nil {
				return ResolvedSourcePosition{}, readErr
			}
			if end, ok := finiteSourceCoverageEnd(track.Source.Descriptor); ok {
				comparison, compareErr := end.Compare(last.Source)
				if compareErr != nil {
					return ResolvedSourcePosition{}, compareErr
				}
				if comparison > 0 {
					delta, subtractErr := end.Subtract(last.Source)
					if subtractErr != nil {
						return ResolvedSourcePosition{}, subtractErr
					}
					proxy, addErr := last.Proxy.Add(delta)
					if addErr != nil {
						return ResolvedSourcePosition{}, addErr
					}
					return ResolvedSourcePosition{
						Source: end, Proxy: proxy, Boundary: SourcePositionCoverageEnd, AtEnd: true,
					}, nil
				}
			}
			last.Boundary = SourcePositionVideoPresentation
			last.AtStart = count == 1
			last.AtEnd = true
			return last, nil
		}
	default:
		return ResolvedSourcePosition{}, ErrMediaLeaseInvalid
	}
	result, err := videoPointPosition(file, track, index)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	result.Boundary = SourcePositionVideoPresentation
	result.AtStart = index == 0
	result.AtEnd = index == count-1
	return result, nil
}

func firstVideoPointAfter(
	file *os.File,
	track application.SourceProxyVideoTrack,
	target domain.RationalTime,
) (uint64, error) {
	low, high := uint64(0), track.FrameCount.Value()
	for low < high {
		middle := low + (high-low)/2
		point, err := application.ReadSourceProxyTimeMapPointAt(file, track.FrameCount.Value(), middle)
		if err != nil {
			return 0, err
		}
		comparison, err := compareTicksToTime(point.SourcePTS, track.Source.Descriptor.TimeBase, target)
		if err != nil {
			return 0, err
		}
		if comparison <= 0 {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low, nil
}

func videoPointPosition(
	file *os.File,
	track application.SourceProxyVideoTrack,
	index uint64,
) (ResolvedSourcePosition, error) {
	point, err := application.ReadSourceProxyTimeMapPointAt(file, track.FrameCount.Value(), index)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	source, err := timeFromTicks(point.SourcePTS, track.Source.Descriptor.TimeBase)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	proxy, err := timeFromTicks(point.ProxyPTS, track.TimeBase)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	return ResolvedSourcePosition{Source: source, Proxy: proxy}, nil
}

func ResolveAudioSourcePosition(
	track application.SourceProxyAudioTrack,
	request SourcePositionRequest,
) (ResolvedSourcePosition, error) {
	if track.Source.Descriptor.Audio == nil || track.Source.Descriptor.Audio.SampleRate == 0 {
		return ResolvedSourcePosition{}, ErrMediaLeaseInvalid
	}
	start := track.SourceStartTime
	endDuration, err := domain.NewRationalTime(int64(track.DecodedSampleCount.Value()), int32(track.SampleRate))
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	end, err := start.Add(endDuration)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	if coverageEnd, ok := finiteSourceCoverageEnd(track.Source.Descriptor); ok {
		comparison, compareErr := coverageEnd.Compare(end)
		if compareErr != nil {
			return ResolvedSourcePosition{}, compareErr
		}
		if comparison < 0 {
			end = coverageEnd
		}
	}
	startComparison, err := request.Target.Compare(start)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	if startComparison <= 0 {
		return ResolvedSourcePosition{
			Source: start, Proxy: track.ProxyStartTime, Boundary: SourcePositionAudioSample,
			AtStart: true, AtEnd: false,
		}, nil
	}
	endComparison, err := request.Target.Compare(end)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	if endComparison >= 0 && request.Operation != SourcePositionPrevious {
		effectiveDuration, subtractErr := end.Subtract(start)
		if subtractErr != nil {
			return ResolvedSourcePosition{}, subtractErr
		}
		proxyEnd, addErr := track.ProxyStartTime.Add(effectiveDuration)
		if addErr != nil {
			return ResolvedSourcePosition{}, addErr
		}
		return ResolvedSourcePosition{
			Source: end, Proxy: proxyEnd, Boundary: SourcePositionCoverageEnd, AtEnd: true,
		}, nil
	}
	elapsed, err := request.Target.Subtract(start)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	index, exact, err := floorRationalUnits(elapsed, track.Source.Descriptor.Audio.SampleRate)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	switch request.Operation {
	case SourcePositionPrevious:
		if exact && index > 0 {
			index--
		}
	case SourcePositionNext:
		index++
	case SourcePositionSettle:
	default:
		return ResolvedSourcePosition{}, ErrMediaLeaseInvalid
	}
	sourceOffset, err := domain.NewRationalTime(int64(index), int32(track.Source.Descriptor.Audio.SampleRate))
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	source, err := start.Add(sourceOffset)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	comparison, err := source.Compare(end)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	boundary := SourcePositionAudioSample
	atEnd := false
	if comparison >= 0 {
		source = end
		boundary = SourcePositionCoverageEnd
		atEnd = true
	}
	proxyOffset, err := source.Subtract(start)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	proxy, err := track.ProxyStartTime.Add(proxyOffset)
	if err != nil {
		return ResolvedSourcePosition{}, err
	}
	return ResolvedSourcePosition{
		Source: source, Proxy: proxy, Boundary: boundary,
		AtStart: index == 0, AtEnd: atEnd,
	}, nil
}

func finiteSourceCoverageEnd(descriptor domain.SourceStreamDescriptor) (domain.RationalTime, bool) {
	if descriptor.Duration == nil {
		return domain.RationalTime{}, false
	}
	start, _ := domain.NewRationalTime(0, 1)
	if descriptor.StartTime != nil {
		start = *descriptor.StartTime
	}
	end, err := start.Add(*descriptor.Duration)
	return end, err == nil
}

func compareTicksToTime(ticks int64, timeBase domain.RationalTime, target domain.RationalTime) (int, error) {
	if timeBase.Validate() != nil || !timeBase.IsPositive() || target.Validate() != nil {
		return 0, ErrMediaLeaseInvalid
	}
	left := new(big.Int).Mul(big.NewInt(ticks), big.NewInt(timeBase.Value.Value()))
	left.Mul(left, big.NewInt(int64(target.Scale)))
	right := new(big.Int).Mul(big.NewInt(target.Value.Value()), big.NewInt(int64(timeBase.Scale)))
	return left.Cmp(right), nil
}

func timeFromTicks(ticks int64, timeBase domain.RationalTime) (domain.RationalTime, error) {
	if timeBase.Validate() != nil || !timeBase.IsPositive() {
		return domain.RationalTime{}, ErrMediaLeaseInvalid
	}
	numerator := new(big.Int).Mul(big.NewInt(ticks), big.NewInt(timeBase.Value.Value()))
	denominator := big.NewInt(int64(timeBase.Scale))
	divisor := new(big.Int).GCD(nil, nil, new(big.Int).Abs(new(big.Int).Set(numerator)), denominator)
	numerator.Quo(numerator, divisor)
	denominator.Quo(denominator, divisor)
	if !numerator.IsInt64() || !denominator.IsInt64() || denominator.Int64() > math.MaxInt32 {
		return domain.RationalTime{}, domain.ErrTimeOverflow
	}
	return domain.NewRationalTime(numerator.Int64(), int32(denominator.Int64()))
}

func floorRationalUnits(value domain.RationalTime, units uint32) (uint64, bool, error) {
	if value.Validate() != nil || value.IsNegative() || units == 0 {
		return 0, false, ErrMediaLeaseInvalid
	}
	numerator := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(int64(units)))
	denominator := big.NewInt(int64(value.Scale))
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if !quotient.IsUint64() || quotient.Uint64() > math.MaxInt64 {
		return 0, false, domain.ErrTimeOverflow
	}
	return quotient.Uint64(), remainder.Sign() == 0, nil
}
