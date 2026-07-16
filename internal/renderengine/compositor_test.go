package renderengine

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestVideoCompositorEmitsExactBlackForTrackGaps(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	plan.Video = nil
	plan.Inputs = nil
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := compositor.CompositeFrame(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSolidYUV420(t, frame, 4, 4, 16, 128, 128)
	if err := compositor.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestVideoCompositorPreservesConstantIdentityWithoutEdgeDarkening(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	source := solidYUV420(4, 4, 235, 128, 128)
	frame, err := compositor.CompositeFrame(0, []DecodedVideoLayer{{InstructionIndex: 0, Frame: source}})
	if err != nil {
		t.Fatal(err)
	}
	assertSolidYUV420(t, frame, 4, 4, 235, 128, 128)
	if err := compositor.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestVideoCompositorUsesStableSourceOverAndRequiresCompleteLayers(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	second := plan.Video[0]
	second.Placement.OpacityBasisPoints = 5_000
	plan.Video = append(plan.Video, second)
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compositor.CompositeFrame(0, []DecodedVideoLayer{{
		InstructionIndex: 0, Frame: solidYUV420(4, 4, 235, 128, 128),
	}}); err == nil {
		t.Fatal("compositor accepted a missing active layer")
	}
	compositor, err = newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := compositor.CompositeFrame(0, []DecodedVideoLayer{
		{InstructionIndex: 0, Frame: solidYUV420(4, 4, 235, 128, 128)},
		{InstructionIndex: 1, Frame: solidYUV420(4, 4, 16, 128, 128)},
	})
	if err != nil {
		t.Fatal(err)
	}
	white := LinearRGBA16{R: 65_535, G: 65_535, B: 65_535, A: 65_535}
	blackHalf := applyLinearOpacity(LinearRGBA16{A: 65_535}, 5_000)
	expected := sourceOverLinear(blackHalf, white)
	yuv := LinearRGB16ToLimitedRec709(RGB16{R: expected.R, G: expected.G, B: expected.B})
	assertSolidYUV420(t, frame, 4, 4, yuv.Y, yuv.Cb, yuv.Cr)
}

func TestVideoCompositorAppliesContinuousCropBeforeFit(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	plan.Video[0].Placement.CropXBasisPoints = 5_000
	plan.Video[0].Placement.CropWidthBasisPoints = 5_000
	plan.Video[0].Placement.AnchorXBasisPoints = 5_000
	plan.Video[0].Placement.FitPolicy = "cover"
	source := solidYUV420(4, 4, 16, 128, 128)
	for y := 0; y < 4; y++ {
		for x := 2; x < 4; x++ {
			source[y*4+x] = 235
		}
	}
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := compositor.CompositeFrame(0, []DecodedVideoLayer{{InstructionIndex: 0, Frame: source}})
	if err != nil {
		t.Fatal(err)
	}
	assertSolidYUV420(t, frame, 4, 4, 235, 128, 128)
}

func TestCaptionCoverageCompositesAfterVideoAndClosesActiveSet(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	plan.Video = nil
	plan.Inputs = nil
	plan.Captions = []domain.RenderCaptionInstruction{{
		Range: domain.TimeRange{
			Start:    domain.RationalTime{Value: 0, Scale: 1},
			Duration: domain.RationalTime{Value: 1, Scale: 1},
		},
		Style: domain.RenderCaptionStyle{
			TextColorRGBA: "#ffffffff", OutlineColorRGBA: "#000000ff",
		},
	}}
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compositor.CompositeFrameWithCaptions(0, nil, nil); err == nil {
		t.Fatal("caption compositor accepted a missing active raster")
	}
	compositor, _ = newVideoCompositor(plan)
	frame, err := compositor.CompositeFrameWithCaptions(0, nil, []CaptionCoverageLayer{{
		InstructionIndex: 0, X: 1, Y: 1, Width: 1, Height: 1, Fill: []byte{255},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			want := byte(16)
			if x == 1 && y == 1 {
				want = 235
			}
			if got := frame[y*4+x]; got != want {
				t.Fatalf("Y[%d,%d]=%d want=%d", x, y, got, want)
			}
		}
	}
	if err := compositor.Finish(); err != nil {
		t.Fatal(err)
	}
}

func solidYUV420(width, height int, y, cb, cr byte) []byte {
	pixels := width * height
	result := make([]byte, pixels+pixels/2)
	for index := 0; index < pixels; index++ {
		result[index] = y
	}
	for index := pixels; index < pixels+pixels/4; index++ {
		result[index] = cb
	}
	for index := pixels + pixels/4; index < len(result); index++ {
		result[index] = cr
	}
	return result
}

func assertSolidYUV420(
	t *testing.T,
	frame []byte,
	width, height int,
	y, cb, cr byte,
) {
	t.Helper()
	pixels := width * height
	if len(frame) != pixels+pixels/2 {
		t.Fatalf("frame bytes=%d", len(frame))
	}
	for index, current := range frame[:pixels] {
		if current != y {
			t.Fatalf("Y[%d]=%d want=%d", index, current, y)
		}
	}
	for index, current := range frame[pixels : pixels+pixels/4] {
		if current != cb {
			t.Fatalf("Cb[%d]=%d want=%d", index, current, cb)
		}
	}
	for index, current := range frame[pixels+pixels/4:] {
		if current != cr {
			t.Fatalf("Cr[%d]=%d want=%d", index, current, cr)
		}
	}
}
