package draftoverlay

import (
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func testClipID(t *testing.T, suffix string) domain.ClipID {
	t.Helper()
	id, err := domain.ParseClipID("00000000-0000-7000-8000-00000000" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testRevision(t *testing.T, value uint64) domain.Revision {
	t.Helper()
	revision, err := domain.NewRevision(value)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func testPlacement(t *testing.T, opacityBasisPoints uint16) domain.RenderPlacement {
	t.Helper()
	unit, err := domain.NewExactRational(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := domain.NewExactRational(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	return domain.RenderPlacement{
		CropWidthBasisPoints: 10_000, CropHeightBasisPoints: 10_000,
		ScaleX: unit, ScaleY: unit, TranslateX: zero, TranslateY: zero,
		AnchorXBasisPoints: 5_000, AnchorYBasisPoints: 5_000,
		OpacityBasisPoints: opacityBasisPoints, FitPolicy: "contain",
	}
}

func testPlan(t *testing.T) domain.RenderPlanPayload {
	t.Helper()
	return domain.RenderPlanPayload{
		SequenceRevision: testRevision(t, 35),
		Video: []domain.RenderVideoInstruction{
			{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
				Placement: testPlacement(t, 10_000),
			},
			{
				ClipID: testClipID(t, "0002"), ClipRevision: testRevision(t, 7),
				Placement: testPlacement(t, 10_000),
			},
		},
	}
}

func TestEmptyOverlayReproducesThePinnedPlanExactly(t *testing.T) {
	plan := testPlan(t)
	applied, err := Apply(plan, Overlay{SequenceRevision: testRevision(t, 35)})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Video) != len(plan.Video) {
		t.Fatalf("instruction count changed: %d", len(applied.Video))
	}
	for index := range plan.Video {
		if applied.Video[index] != plan.Video[index] {
			t.Fatalf("instruction %d changed under an empty overlay", index)
		}
	}
}

func TestEmptyOverlayIgnoresASequenceRevisionMismatch(t *testing.T) {
	plan := testPlan(t)
	if _, err := Apply(plan, Overlay{SequenceRevision: testRevision(t, 34)}); err != nil {
		t.Fatalf("an empty overlay carries no base revisions to be stale against: %v", err)
	}
}

func TestApplySubstitutesOnlyTheTargetedInstruction(t *testing.T) {
	plan := testPlan(t)
	overlay := Overlay{
		SequenceRevision: testRevision(t, 35),
		Placements: []ClipPlacementOperation{{
			ClipID: testClipID(t, "0002"), ClipRevision: testRevision(t, 7),
			Placement: testPlacement(t, 6_000),
		}},
	}
	applied, err := Apply(plan, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Video[1].Placement.OpacityBasisPoints != 6_000 {
		t.Fatalf("target placement was not applied: %+v", applied.Video[1].Placement)
	}
	if applied.Video[0] != plan.Video[0] {
		t.Fatal("an untargeted instruction changed")
	}
	if applied.Video[1].ClipID != plan.Video[1].ClipID ||
		applied.Video[1].ClipRevision != plan.Video[1].ClipRevision {
		t.Fatal("applying a placement changed the instruction's identity")
	}
}

func TestApplyDoesNotMutateThePinnedPlan(t *testing.T) {
	plan := testPlan(t)
	overlay := Overlay{
		SequenceRevision: testRevision(t, 35),
		Placements: []ClipPlacementOperation{{
			ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
			Placement: testPlacement(t, 2_500),
		}},
	}
	if _, err := Apply(plan, overlay); err != nil {
		t.Fatal(err)
	}
	if plan.Video[0].Placement.OpacityBasisPoints != 10_000 {
		t.Fatal("applying an overlay mutated the pinned plan")
	}
}

func TestStaleOverlaysAreRejectedWhole(t *testing.T) {
	plan := testPlan(t)
	cases := map[string]Overlay{
		"sequence revision moved": {
			SequenceRevision: testRevision(t, 34),
			Placements: []ClipPlacementOperation{{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
				Placement: testPlacement(t, 5_000),
			}},
		},
		"clip revision moved": {
			SequenceRevision: testRevision(t, 35),
			Placements: []ClipPlacementOperation{{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 3),
				Placement: testPlacement(t, 5_000),
			}},
		},
		"clip absent from the plan": {
			SequenceRevision: testRevision(t, 35),
			Placements: []ClipPlacementOperation{{
				ClipID: testClipID(t, "0009"), ClipRevision: testRevision(t, 1),
				Placement: testPlacement(t, 5_000),
			}},
		},
	}
	for name, overlay := range cases {
		if _, err := Apply(plan, overlay); !errors.Is(err, ErrOverlayStale) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestOneStaleOperationRejectsTheWholeOverlay(t *testing.T) {
	plan := testPlan(t)
	overlay := Overlay{
		SequenceRevision: testRevision(t, 35),
		Placements: []ClipPlacementOperation{
			{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
				Placement: testPlacement(t, 1_000),
			},
			{
				ClipID: testClipID(t, "0002"), ClipRevision: testRevision(t, 6),
				Placement: testPlacement(t, 1_000),
			},
		},
	}
	if _, err := Apply(plan, overlay); !errors.Is(err, ErrOverlayStale) {
		t.Fatalf("err=%v", err)
	}
	if plan.Video[0].Placement.OpacityBasisPoints != 10_000 {
		t.Fatal("a rejected overlay was partially applied")
	}
}

func TestInvalidOverlaysAreRejectedBeforeStalenessIsConsidered(t *testing.T) {
	plan := testPlan(t)
	duplicate := Overlay{
		SequenceRevision: testRevision(t, 35),
		Placements: []ClipPlacementOperation{
			{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
				Placement: testPlacement(t, 1_000),
			},
			{
				ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
				Placement: testPlacement(t, 2_000),
			},
		},
	}
	if _, err := Apply(plan, duplicate); !errors.Is(err, ErrOverlayInvalid) {
		t.Fatalf("duplicate clip: err=%v", err)
	}
	outOfContract := testPlacement(t, 10_000)
	outOfContract.FitPolicy = "stretch"
	beyond := Overlay{
		SequenceRevision: testRevision(t, 35),
		Placements: []ClipPlacementOperation{{
			ClipID: testClipID(t, "0001"), ClipRevision: testRevision(t, 4),
			Placement: outOfContract,
		}},
	}
	if _, err := Apply(plan, beyond); !errors.Is(err, ErrOverlayInvalid) {
		t.Fatalf("out-of-contract placement: err=%v", err)
	}
	tooMany := Overlay{SequenceRevision: testRevision(t, 35)}
	for index := 0; index <= MaximumOverlayOperations; index++ {
		tooMany.Placements = append(tooMany.Placements, ClipPlacementOperation{
			ClipID: testClipID(t, "0001"), Placement: testPlacement(t, 1_000),
		})
	}
	if _, err := Apply(plan, tooMany); !errors.Is(err, ErrOverlayInvalid) {
		t.Fatalf("bounded operation set: err=%v", err)
	}
}
