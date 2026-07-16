package businessacceptance

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
)

func parseExactRange(value any) (ExactRangeEvidence, bool) {
	input := record(value)
	start, startOK := parseExactTime(input["start"])
	duration, durationOK := parseExactTime(input["duration"])
	if !startOK || !durationOK || !exactTimePositive(duration) {
		return ExactRangeEvidence{}, false
	}
	return ExactRangeEvidence{Start: start, Duration: duration}, true
}

func parseExactTime(value any) (ExactTimeEvidence, bool) {
	input := record(value)
	numerator, numeratorOK := input["value"].(string)
	scaleNumber, scaleOK := input["scale"].(json.Number)
	if !numeratorOK || !scaleOK {
		return ExactTimeEvidence{}, false
	}
	parsedNumerator, ok := new(big.Int).SetString(numerator, 10)
	if !ok || parsedNumerator.String() != numerator || !parsedNumerator.IsInt64() {
		return ExactTimeEvidence{}, false
	}
	scale, err := strconv.ParseInt(scaleNumber.String(), 10, 32)
	if err != nil || scale < 1 || strconv.FormatInt(scale, 10) != scaleNumber.String() {
		return ExactTimeEvidence{}, false
	}
	result := ExactTimeEvidence{Value: numerator, Scale: int32(scale)}
	canonical, canonicalOK := exactTimeFromRat(exactTimeRat(result))
	return result, canonicalOK && canonical == result
}

func exactRangeEnd(value ExactRangeEvidence) (ExactTimeEvidence, bool) {
	return exactTimeFromRat(new(big.Rat).Add(exactTimeRat(value.Start), exactTimeRat(value.Duration)))
}

func exactRangeFromBounds(start, end ExactTimeEvidence) (ExactRangeEvidence, bool) {
	duration, ok := exactTimeFromRat(new(big.Rat).Sub(exactTimeRat(end), exactTimeRat(start)))
	if !ok || !exactTimePositive(duration) {
		return ExactRangeEvidence{}, false
	}
	return ExactRangeEvidence{Start: start, Duration: duration}, true
}

func exactTimeRat(value ExactTimeEvidence) *big.Rat {
	numerator, _ := new(big.Int).SetString(value.Value, 10)
	return new(big.Rat).SetFrac(numerator, big.NewInt(int64(value.Scale)))
}

func exactTimeFromRat(value *big.Rat) (ExactTimeEvidence, bool) {
	if value == nil || !value.Num().IsInt64() || !value.Denom().IsInt64() {
		return ExactTimeEvidence{}, false
	}
	scale := value.Denom().Int64()
	if scale < 1 || scale > math.MaxInt32 {
		return ExactTimeEvidence{}, false
	}
	return ExactTimeEvidence{Value: value.Num().String(), Scale: int32(scale)}, true
}

func exactTimePositive(value ExactTimeEvidence) bool {
	numerator, ok := new(big.Int).SetString(value.Value, 10)
	return ok && numerator.Sign() > 0
}

func exactTimeCompare(left, right ExactTimeEvidence) int {
	return exactTimeRat(left).Cmp(exactTimeRat(right))
}

func exactRangeEqual(value any, expected ExactRangeEvidence) bool {
	actual, ok := parseExactRange(value)
	return ok && actual == expected
}

func exactTimeArgument(value ExactTimeEvidence) string {
	return value.Value + "/" + strconv.FormatInt(int64(value.Scale), 10)
}
