package renderengine

import (
	"errors"
	"reflect"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestVideoResamplePlanUsesContinuousPixelCentersAndExactTapWork(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	result, err := CompileVideoResampleInstructionPlan(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []ResampleSourceSpan{
		{First: 0, Count: 2, KernelFirst: -1, KernelCount: 3},
		{First: 0, Count: 3, KernelFirst: 0, KernelCount: 3},
		{First: 1, Count: 3, KernelFirst: 1, KernelCount: 3},
		{First: 2, Count: 2, KernelFirst: 2, KernelCount: 3},
	}
	if result.Policy != ResamplePlanPolicyV1 || !reflect.DeepEqual(result.Horizontal.Samples, want) ||
		!reflect.DeepEqual(result.Vertical.Samples, want) || result.Horizontal.TotalTaps != 10 ||
		result.Horizontal.ActiveOutputSamples != 4 || result.Horizontal.UniqueSourceSamples != 4 ||
		result.Horizontal.MaximumTaps != 3 || result.Horizontal.MaximumKernelTaps != 3 || result.ActiveFrames != 1 ||
		result.TapWorkPerFrame != 80 || result.TapWork != 80 {
		t.Fatalf("result=%+v", result)
	}
	work, err := compileResampleTapWork(plan)
	if err != nil || work != result.TapWork {
		t.Fatalf("work=%d err=%v", work, err)
	}
}

func TestMitchellQ30WeightsNormalizeIdentityCropWithoutDarkEdges(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	result, err := CompileVideoResampleInstructionPlan(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	edge, err := CompileResampleAxisWeights(result.Horizontal, 0)
	if err != nil {
		t.Fatal(err)
	}
	if edge.First != 0 || !reflect.DeepEqual(edge.Coefficients, []int64{1_010_580_540, 63_161_284}) ||
		edge.Coefficients[0]+edge.Coefficients[1] != resampleCoefficientOneQ30 {
		t.Fatalf("edge=%+v", edge)
	}
	interior, err := CompileResampleAxisWeights(result.Horizontal, 1)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, coefficient := range interior.Coefficients {
		sum += coefficient
	}
	if sum != resampleCoefficientOneQ30 {
		t.Fatalf("interior=%+v sum=%d", interior, sum)
	}
}

func TestVideoResamplePlanWidensMinificationAndRejectsUnboundedAxis(t *testing.T) {
	plan := resampleFixturePlan(t, 8, 8, 4, 4)
	result, err := CompileVideoResampleInstructionPlan(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Horizontal.MaximumTaps != 7 || result.Horizontal.MaximumKernelTaps != 8 || result.Horizontal.TotalTaps != 24 ||
		result.Horizontal.UniqueSourceSamples != 8 || result.TapWorkPerFrame != 288 {
		t.Fatalf("result=%+v", result)
	}
	plan = resampleFixturePlan(t, 16_384, 16_384, 2, 2)
	_, err = CompileVideoResampleInstructionPlan(plan, 0)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Subject != "resample-axis-taps" {
		t.Fatalf("limit=%+v err=%v", limit, err)
	}
}

func TestVideoResamplePlanTreatsCropExteriorAsTransparent(t *testing.T) {
	plan := resampleFixturePlan(t, 4, 4, 4, 4)
	placement := &plan.Video[0].Placement
	placement.CropXBasisPoints = 5_000
	placement.CropWidthBasisPoints = 5_000
	placement.AnchorXBasisPoints = 0
	result, err := CompileVideoResampleInstructionPlan(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, span := range result.Horizontal.Samples {
		if span.Count > 0 && span.First < 2 {
			t.Fatalf("crop-exterior source entered the span: %+v", result.Horizontal.Samples)
		}
	}
}

func resampleFixturePlan(
	t *testing.T,
	sourceWidth, sourceHeight, canvasWidth, canvasHeight uint32,
) domain.RenderPlanPayload {
	t.Helper()
	artifactID := mustRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000090")
	streamID := mustRenderID(t, domain.ParseSourceStreamID, "00000000-0000-7000-8000-000000000091")
	one, _ := domain.NewExactRational(1, 1)
	zero, _ := domain.NewExactRational(0, 1)
	oneSecond, _ := domain.NewRationalTime(1, 1)
	frameCount, _ := domain.NewUInt64(1)
	return domain.RenderPlanPayload{
		Inputs: []domain.RenderPlanInput{{
			ArtifactID: artifactID,
			Video: &domain.RenderVideoInput{
				SourceStreamID: streamID, Width: sourceWidth, Height: sourceHeight,
			},
		}},
		Video: []domain.RenderVideoInstruction{{
			InputArtifactID: artifactID, SourceStreamID: streamID,
			TimelineRange: domain.TimeRange{Start: domain.RationalTime{Value: 0, Scale: 1}, Duration: oneSecond},
			Orientation:   "normalized-by-render-material-v1",
			Placement: domain.RenderPlacement{
				CropWidthBasisPoints: 10_000, CropHeightBasisPoints: 10_000,
				ScaleX: one, ScaleY: one, TranslateX: zero, TranslateY: zero,
				AnchorXBasisPoints: 5_000, AnchorYBasisPoints: 5_000,
				OpacityBasisPoints: 10_000, FitPolicy: "contain",
			},
		}},
		Output: domain.RenderOutputPolicy{
			CanvasWidth: canvasWidth, CanvasHeight: canvasHeight,
			FrameRate: oneSecond, VideoFrameCount: frameCount,
		},
	}
}
