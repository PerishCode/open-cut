package renderengine

import (
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	Rec709IntegerOracleV1 = domain.RenderColorPipelineV1
	AudioGainIntegerV1    = domain.RenderAudioGainPolicyV1

	rec709KnotCount         = 4097
	rec709TableBytes        = rec709KnotCount * 2
	rec709LUTBytes          = rec709TableBytes * 2
	rec709LUTSHA256         = "7d34de8c59024b3ad796f83b857db23ba90ba7311c754909290e69936e72d51d"
	gainFractionBits        = 56
	gainOneFixed     uint64 = 1 << gainFractionBits
	gainStepFixed    uint64 = 72_065_890_452_592_228
	gainInverseFixed uint64 = 72_049_298_578_368_699

	MaximumActiveAudioLayers = 32
)

var ErrIntegerOracleInput = errors.New("integer render oracle input is invalid")

//go:embed rec709_lut.bin
var rec709LUT []byte

type RGB16 struct {
	R uint16
	G uint16
	B uint16
}

type YUV8 struct {
	Y  uint8
	Cb uint8
	Cr uint8
}

func ValidateIntegerOracle() error {
	if len(rec709LUT) != rec709LUTBytes {
		return fmt.Errorf("Rec.709 integer oracle LUT size is invalid")
	}
	digest := sha256.Sum256(rec709LUT)
	if fmt.Sprintf("%x", digest) != rec709LUTSHA256 {
		return fmt.Errorf("Rec.709 integer oracle LUT digest is invalid")
	}
	return nil
}

func LimitedRec709ToLinearRGB16(input YUV8) (RGB16, error) {
	if input.Y < 16 || input.Y > 235 || input.Cb < 16 || input.Cb > 240 || input.Cr < 16 || input.Cr > 240 {
		return RGB16{}, fmt.Errorf(
			"%w: limited-range Rec.709 sample out of range Y=%d Cb=%d Cr=%d",
			ErrIntegerOracleInput, input.Y, input.Cb, input.Cr,
		)
	}
	gamma := limitedRec709ToGammaRGB16(input)
	return RGB16{
		R: rec709Lookup(gamma.R, 0),
		G: rec709Lookup(gamma.G, 0),
		B: rec709Lookup(gamma.B, 0),
	}, nil
}

func LinearRGB16ToLimitedRec709(input RGB16) YUV8 {
	gamma := RGB16{
		R: rec709Lookup(input.R, rec709TableBytes),
		G: rec709Lookup(input.G, rec709TableBytes),
		B: rec709Lookup(input.B, rec709TableBytes),
	}
	return gammaRGB16ToLimitedRec709(gamma)
}

func limitedRec709ToGammaRGB16(input YUV8) RGB16 {
	y := int64(input.Y) - 16
	cb := int64(input.Cb) - 128
	cr := int64(input.Cr) - 128
	const redBlueDenominator = int64(245_280_000)
	redNumerator := (y*1_120_000 + cr*7_874*219) * 65_535
	blueNumerator := (y*1_120_000 + cb*9_278*219) * 65_535
	const greenDenominator = int64(146_186_880_000)
	greenNumerator := (y*667_520_000 - cb*1_674_679*73 - cr*4_185_031*73) * 65_535
	return RGB16{
		R: clampUint16(roundHalfEvenSigned(redNumerator, redBlueDenominator)),
		G: clampUint16(roundHalfEvenSigned(greenNumerator, greenDenominator)),
		B: clampUint16(roundHalfEvenSigned(blueNumerator, redBlueDenominator)),
	}
}

func gammaRGB16ToLimitedRec709(input RGB16) YUV8 {
	r, g, b := int64(input.R), int64(input.G), int64(input.B)
	yNumerator := int64(219) * (1_063*r + 3_576*g + 361*b)
	cbNumerator := int64(224) * (1_063*(b-r) + 3_576*(b-g))
	crNumerator := int64(224) * (3_576*(r-g) + 361*(r-b))
	return YUV8{
		Y:  clampUint8(16+roundHalfEvenSigned(yNumerator, 5_000*65_535), 16, 235),
		Cb: clampUint8(128+roundHalfEvenSigned(cbNumerator, 9_278*65_535), 16, 240),
		Cr: clampUint8(128+roundHalfEvenSigned(crNumerator, 7_874*65_535), 16, 240),
	}
}

// ReconstructLeftChroma420 implements left-center siting: the two luma rows
// share one chroma row, even columns are co-sited, and odd columns use a
// round-half-even horizontal midpoint with edge clamp.
func ReconstructLeftChroma420(plane []byte, chromaWidth, chromaHeight, x, y int) (uint8, error) {
	if chromaWidth <= 0 || chromaHeight <= 0 || len(plane) != chromaWidth*chromaHeight || x < 0 || y < 0 ||
		x/2 >= chromaWidth || y/2 >= chromaHeight {
		return 0, fmt.Errorf(
			"%w: chroma reconstruct bounds w=%d h=%d len=%d x=%d y=%d",
			ErrIntegerOracleInput, chromaWidth, chromaHeight, len(plane), x, y,
		)
	}
	row, column := y/2, x/2
	left := plane[row*chromaWidth+column]
	if x%2 == 0 || column+1 == chromaWidth {
		return left, nil
	}
	right := plane[row*chromaWidth+column+1]
	return uint8(roundHalfEvenSigned(int64(left)+int64(right), 2)), nil
}

// DownsampleLeftChroma420 applies a centered horizontal [1 2 1] triangle at
// each left co-site and a two-row box, with edge clamp and one final
// round-half-even division by eight.
func DownsampleLeftChroma420(full []uint16, width, height int) ([]uint16, error) {
	if width <= 0 || height <= 0 || len(full) != width*height {
		return nil, fmt.Errorf(
			"%w: chroma downsample shape w=%d h=%d len=%d",
			ErrIntegerOracleInput, width, height, len(full),
		)
	}
	chromaWidth, chromaHeight := (width+1)/2, (height+1)/2
	result := make([]uint16, chromaWidth*chromaHeight)
	for cy := 0; cy < chromaHeight; cy++ {
		firstRow := cy * 2
		secondRow := firstRow + 1
		if secondRow >= height {
			secondRow = height - 1
		}
		for cx := 0; cx < chromaWidth; cx++ {
			center := cx * 2
			left, right := center-1, center+1
			if left < 0 {
				left = 0
			}
			if right >= width {
				right = width - 1
			}
			sum := int64(full[firstRow*width+left]) + 2*int64(full[firstRow*width+center]) + int64(full[firstRow*width+right])
			sum += int64(full[secondRow*width+left]) + 2*int64(full[secondRow*width+center]) + int64(full[secondRow*width+right])
			result[cy*chromaWidth+cx] = uint16(roundHalfEvenSigned(sum, 8))
		}
	}
	return result, nil
}

func GainCoefficientQ31(milliDB int32) (int64, error) {
	if milliDB < -96_000 || milliDB > 24_000 {
		return 0, ErrIntegerOracleInput
	}
	if milliDB == 0 {
		return 1 << 31, nil
	}
	exponent := int64(milliDB)
	factor := gainStepFixed
	if exponent < 0 {
		exponent = -exponent
		factor = gainInverseFixed
	}
	value := gainOneFixed
	for exponent > 0 {
		if exponent&1 != 0 {
			value = multiplyGainFixedHalfEven(value, factor)
		}
		exponent >>= 1
		if exponent > 0 {
			factor = multiplyGainFixedHalfEven(factor, factor)
		}
	}
	return int64(roundShiftHalfEven(value, gainFractionBits-31)), nil
}

func MixPCM16(samples []int16, gainQ31 []int64) (int16, error) {
	if len(samples) != len(gainQ31) || len(samples) > MaximumActiveAudioLayers {
		return 0, ErrIntegerOracleInput
	}
	var sum int64
	for index, sample := range samples {
		if gainQ31[index] < 0 || gainQ31[index] > 34_035_322_146 {
			return 0, ErrIntegerOracleInput
		}
		product := int64(sample) * gainQ31[index]
		if product > 0 && sum > int64(^uint64(0)>>1)-product || product < 0 && sum < -int64(^uint64(0)>>1)-1-product {
			return 0, ErrIntegerOracleInput
		}
		sum += product
	}
	mixed := roundHalfEvenSigned(sum, 1<<31)
	if mixed > 32_767 {
		return 32_767, nil
	}
	if mixed < -32_768 {
		return -32_768, nil
	}
	return int16(mixed), nil
}

func rec709Lookup(value uint16, offset int) uint16 {
	index := int(value) >> 4
	fraction := int64(int(value) & 15)
	denominator := int64(16)
	if index == rec709KnotCount-2 {
		fraction = int64(int(value) - index*16)
		denominator = 15
	}
	left := int64(binary.BigEndian.Uint16(rec709LUT[offset+index*2:]))
	if fraction == 0 {
		return uint16(left)
	}
	right := int64(binary.BigEndian.Uint16(rec709LUT[offset+(index+1)*2:]))
	return uint16(left + roundHalfEvenSigned((right-left)*fraction, denominator))
}

func multiplyGainFixedHalfEven(left, right uint64) uint64 {
	high, low := bits.Mul64(left, right)
	quotient := high<<(64-gainFractionBits) | low>>gainFractionBits
	remainder := low & (uint64(1)<<gainFractionBits - 1)
	half := uint64(1) << (gainFractionBits - 1)
	if remainder > half || remainder == half && quotient&1 != 0 {
		quotient++
	}
	return quotient
}

func roundShiftHalfEven(value uint64, shift uint) uint64 {
	quotient := value >> shift
	remainder := value & (uint64(1)<<shift - 1)
	half := uint64(1) << (shift - 1)
	if remainder > half || remainder == half && quotient&1 != 0 {
		quotient++
	}
	return quotient
}

func roundHalfEvenSigned(numerator, denominator int64) int64 {
	quotient := numerator / denominator
	remainder := numerator % denominator
	if remainder < 0 {
		remainder = -remainder
	}
	comparison := remainder * 2
	if comparison > denominator || comparison == denominator && quotient&1 != 0 {
		if numerator < 0 {
			quotient--
		} else {
			quotient++
		}
	}
	return quotient
}

func clampUint16(value int64) uint16 {
	if value < 0 {
		return 0
	}
	if value > 65_535 {
		return 65_535
	}
	return uint16(value)
}

func clampUint8(value int64, minimum, maximum uint8) uint8 {
	if value < int64(minimum) {
		return minimum
	}
	if value > int64(maximum) {
		return maximum
	}
	return uint8(value)
}
