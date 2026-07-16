package renderengine

import (
	"fmt"
	"math/big"
)

const (
	resampleCoefficientOneQ30     int64 = 1 << 30
	maximumResampleCoefficientQ30 int64 = 2 << 30
)

type ResampleAxisWeights struct {
	First        uint32
	Coefficients []int64
}

// CompileResampleAxisWeights returns coefficients for source samples inside the
// continuous crop. The inverse-mapped output center has already been proven
// inside the crop; retained samples normalize to exact Q30 unity so a full
// identity crop preserves a constant raster without dark borders.
func CompileResampleAxisWeights(plan ResampleAxisPlan, output uint32) (ResampleAxisWeights, error) {
	if int(output) >= len(plan.Samples) || len(plan.sourcePositions) != len(plan.Samples) ||
		plan.sourcePositions[output] == nil || plan.filterScale == nil || plan.filterScale.Sign() <= 0 {
		return ResampleAxisWeights{}, fmt.Errorf("resample axis weight input is invalid")
	}
	span := plan.Samples[output]
	if span.Count == 0 {
		return ResampleAxisWeights{First: span.First, Coefficients: []int64{}}, nil
	}
	if span.KernelCount == 0 || span.KernelCount > MaximumResampleAxisTaps ||
		int64(span.First) < int64(span.KernelFirst) ||
		int64(span.First)+int64(span.Count) > int64(span.KernelFirst)+int64(span.KernelCount) {
		return ResampleAxisWeights{}, fmt.Errorf("resample axis weight span is invalid")
	}
	raw := make([]*big.Rat, span.Count)
	sum := new(big.Rat)
	adjust := 0
	for index := range raw {
		source := int64(span.First) + int64(index)
		center := new(big.Rat).SetFrac(big.NewInt(source*2+1), big.NewInt(2))
		distance := new(big.Rat).Sub(center, plan.sourcePositions[output])
		distance.Mul(distance, plan.filterScale)
		raw[index] = mitchellKernel(distance)
		sum.Add(sum, raw[index])
		if absoluteRational(raw[index]).Cmp(absoluteRational(raw[adjust])) > 0 {
			adjust = index
		}
	}
	if sum.Sign() <= 0 {
		return ResampleAxisWeights{}, fmt.Errorf("resample axis kernel normalization is invalid")
	}
	coefficients := make([]int64, span.Count)
	var coefficientSum int64
	for index := range raw {
		normalized := new(big.Rat).Quo(raw[index], sum)
		normalized.Mul(normalized, new(big.Rat).SetInt64(resampleCoefficientOneQ30))
		value := roundRationalHalfEven(normalized)
		if !value.IsInt64() {
			return ResampleAxisWeights{}, fmt.Errorf("resample axis coefficient overflows")
		}
		coefficients[index] = value.Int64()
		if coefficients[index] < -maximumResampleCoefficientQ30 ||
			coefficients[index] > maximumResampleCoefficientQ30 {
			return ResampleAxisWeights{}, fmt.Errorf("resample axis coefficient exceeds its bound")
		}
		coefficientSum += coefficients[index]
	}
	coefficients[adjust] += resampleCoefficientOneQ30 - coefficientSum
	if coefficients[adjust] < -maximumResampleCoefficientQ30 ||
		coefficients[adjust] > maximumResampleCoefficientQ30 {
		return ResampleAxisWeights{}, fmt.Errorf("resample axis adjusted coefficient exceeds its bound")
	}
	return ResampleAxisWeights{
		First:        span.First,
		Coefficients: coefficients,
	}, nil
}

func mitchellKernel(value *big.Rat) *big.Rat {
	x := absoluteRational(value)
	if x.Cmp(big.NewRat(2, 1)) >= 0 {
		return new(big.Rat)
	}
	x2 := new(big.Rat).Mul(x, x)
	x3 := new(big.Rat).Mul(x2, x)
	if x.Cmp(big.NewRat(1, 1)) < 0 {
		result := new(big.Rat).Mul(big.NewRat(7, 6), x3)
		result.Sub(result, new(big.Rat).Mul(big.NewRat(2, 1), x2))
		return result.Add(result, big.NewRat(8, 9))
	}
	result := new(big.Rat).Mul(big.NewRat(-7, 18), x3)
	result.Add(result, new(big.Rat).Mul(big.NewRat(2, 1), x2))
	result.Sub(result, new(big.Rat).Mul(big.NewRat(10, 3), x))
	return result.Add(result, big.NewRat(16, 9))
}

func roundRationalHalfEven(value *big.Rat) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	absoluteRemainder := new(big.Int).Abs(remainder)
	comparison := new(big.Int).Lsh(absoluteRemainder, 1).Cmp(value.Denom())
	if comparison > 0 || comparison == 0 && quotient.Bit(0) != 0 {
		if value.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return quotient
}

func absoluteRational(value *big.Rat) *big.Rat {
	if value.Sign() < 0 {
		return new(big.Rat).Neg(value)
	}
	return new(big.Rat).Set(value)
}
