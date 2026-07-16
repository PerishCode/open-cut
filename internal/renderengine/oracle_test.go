package renderengine

import (
	"reflect"
	"testing"
)

func TestRec709IntegerOracleGoldenVectors(t *testing.T) {
	if err := ValidateIntegerOracle(); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		input YUV8
		want  RGB16
	}{
		{input: YUV8{Y: 16, Cb: 128, Cr: 128}, want: RGB16{}},
		{input: YUV8{Y: 235, Cb: 128, Cr: 128}, want: RGB16{R: 65535, G: 65535, B: 65535}},
		{input: YUV8{Y: 81, Cb: 120, Cr: 140}, want: RGB16{R: 10408, G: 6095, B: 4509}},
		{input: YUV8{Y: 145, Cb: 150, Cr: 110}, want: RGB16{R: 14736, G: 24609, B: 39019}},
	} {
		got, err := LimitedRec709ToLinearRGB16(fixture.input)
		if err != nil || got != fixture.want {
			t.Fatalf("input=%+v got=%+v want=%+v err=%v", fixture.input, got, fixture.want, err)
		}
	}
	for _, input := range []YUV8{
		{Y: 16, Cb: 128, Cr: 128}, {Y: 235, Cb: 128, Cr: 128},
		{Y: 81, Cb: 120, Cr: 140}, {Y: 145, Cb: 150, Cr: 110},
	} {
		linear, err := LimitedRec709ToLinearRGB16(input)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip := LinearRGB16ToLimitedRec709(linear)
		if absoluteByteDifference(roundTrip.Y, input.Y) > 1 ||
			absoluteByteDifference(roundTrip.Cb, input.Cb) > 1 ||
			absoluteByteDifference(roundTrip.Cr, input.Cr) > 1 {
			t.Fatalf("input=%+v linear=%+v roundTrip=%+v", input, linear, roundTrip)
		}
	}
}

func TestLeftChromaIntegerPolicies(t *testing.T) {
	plane := []byte{10, 11, 14, 20, 21, 24}
	if even, err := ReconstructLeftChroma420(plane, 3, 2, 2, 0); err != nil || even != 11 {
		t.Fatalf("even=%d err=%v", even, err)
	}
	if tieEven, err := ReconstructLeftChroma420(plane, 3, 2, 1, 0); err != nil || tieEven != 10 {
		t.Fatalf("tieEven=%d err=%v", tieEven, err)
	}
	if edge, err := ReconstructLeftChroma420(plane, 3, 2, 5, 3); err != nil || edge != 24 {
		t.Fatalf("edge=%d err=%v", edge, err)
	}
	downsampled, err := DownsampleLeftChroma420([]uint16{
		10, 20, 30, 40,
		14, 24, 34, 44,
		50, 60, 70, 80,
	}, 4, 3)
	if err != nil || !reflect.DeepEqual(downsampled, []uint16{14, 32, 52, 70}) {
		t.Fatalf("downsampled=%v err=%v", downsampled, err)
	}
}

func TestMilliDBQ31AndFinalMixGoldenVectors(t *testing.T) {
	unity, err := GainCoefficientQ31(0)
	if err != nil || unity != 1<<31 {
		t.Fatalf("unity=%d err=%v", unity, err)
	}
	minimum, err := GainCoefficientQ31(-96_000)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := GainCoefficientQ31(24_000)
	if err != nil {
		t.Fatal(err)
	}
	if minimum != 34_035 || maximum != 34_035_322_146 {
		t.Fatalf("minimum=%d maximum=%d", minimum, maximum)
	}
	mixed, err := MixPCM16([]int16{10_000, -5_000}, []int64{unity, unity})
	if err != nil || mixed != 5_000 {
		t.Fatalf("mixed=%d err=%v", mixed, err)
	}
	clipped, err := MixPCM16([]int16{32_767, 32_767}, []int64{unity, unity})
	if err != nil || clipped != 32_767 {
		t.Fatalf("clipped=%d err=%v", clipped, err)
	}
}

func absoluteByteDifference(left, right uint8) uint8 {
	if left > right {
		return left - right
	}
	return right - left
}
