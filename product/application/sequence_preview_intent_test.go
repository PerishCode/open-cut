package application

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewRenderIntentIsCanonicalExactAndClosed(t *testing.T) {
	fixture := renderPlanFixture(t)
	firstJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000020")
	secondJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000021")
	inputs := []SequencePreviewInputRequirement{
		{ClipID: fixture.Clips[1].ID, SourceStreamID: fixture.Clips[1].SourceStreamID, ProducerJobID: secondJob},
		{ClipID: fixture.Clips[0].ID, SourceStreamID: fixture.Clips[0].SourceStreamID, ProducerJobID: firstJob},
	}
	snapshot := SequencePreviewPreparationSnapshot{
		ProjectID: fixture.ProjectID, ObservedProjectRevision: fixture.ObservedProjectRevision,
		Sequence: fixture.Sequence, Clips: fixture.Clips, Captions: fixture.Captions,
		Assets: fixture.Assets,
	}
	intent, canonical, digest, err := NewSequencePreviewRenderIntent(snapshot, inputs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Sequence.Name = "a later display-only name"
	snapshot.Sequence.Tracks[0].Label = "a later display-only label"
	snapshot.Clips[0], snapshot.Clips[1] = snapshot.Clips[1], snapshot.Clips[0]
	inputs[0], inputs[1] = inputs[1], inputs[0]
	_, reorderedCanonical, reorderedDigest, err := NewSequencePreviewRenderIntent(snapshot, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, reorderedCanonical) || digest != reorderedDigest ||
		strings.Contains(string(canonical), "linkGroup") || strings.Contains(string(canonical), "display-only") {
		t.Fatalf("render intent retained ordering or edit-only metadata: %s", canonical)
	}
	decoded, decodedDigest, err := DecodeSequencePreviewRenderIntent(canonical, inputs)
	if err != nil || decodedDigest != digest {
		t.Fatalf("decode exact intent: %v / %s", err, decodedDigest)
	}
	compiledFromIntent, err := CompileSequencePreviewPlan(decoded.CompileInput(fixture.Bindings, nil))
	if err != nil {
		t.Fatal(err)
	}
	compiledFromSnapshot, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualCompiledRenderPlans(compiledFromIntent, compiledFromSnapshot) {
		t.Fatal("durable intent did not reconstruct the exact semantic RenderPlan input")
	}

	mutated := intent
	mutated.Clips = append([]SequencePreviewIntentClip(nil), intent.Clips...)
	one, _ := domain.NewRationalTime(1, 1)
	mutated.Clips[0].TimelineRange.Start = one
	_, _, mutatedDigest, err := CanonicalSequencePreviewRenderIntent(mutated, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if mutatedDigest == digest {
		t.Fatal("semantic timeline mutation did not change the render intent digest")
	}

	unknownField := bytes.Replace(canonical, []byte(`"payload":{`), []byte(`"payload":{"unknown":true,`), 1)
	if _, _, err := DecodeSequencePreviewRenderIntent(unknownField, inputs); err == nil {
		t.Fatal("render intent accepted an unknown field")
	}
}

func TestSequencePreviewIntentCarriesClipPlacementRoundTrip(t *testing.T) {
	fixture := renderPlanFixture(t)
	placement := domain.IdentityClipPlacement()
	placement.OpacityBasisPoints = 6_000
	placement.ScaleX = domain.ExactRational{Value: 1, Scale: 2}
	clips := append([]domain.ClipState(nil), fixture.Clips...)
	clips[0].Placement = &placement
	snapshot := SequencePreviewPreparationSnapshot{
		ProjectID: fixture.ProjectID, ObservedProjectRevision: fixture.ObservedProjectRevision,
		Sequence: fixture.Sequence, Clips: clips, Captions: fixture.Captions,
		Assets: fixture.Assets,
	}

	firstJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000030")
	secondJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000031")
	inputs := []SequencePreviewInputRequirement{
		{ClipID: clips[0].ID, SourceStreamID: clips[0].SourceStreamID, ProducerJobID: firstJob},
		{ClipID: clips[1].ID, SourceStreamID: clips[1].SourceStreamID, ProducerJobID: secondJob},
	}
	intent, encoded, _, err := NewSequencePreviewRenderIntent(snapshot, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"opacityBasisPoints":6000`)) {
		t.Fatalf("intent wire must carry the placement, got %s", encoded)
	}
	input := intent.CompileInput(nil, nil)
	var placed *domain.ClipPlacement
	for _, clip := range input.Clips {
		if clip.ID == clips[0].ID {
			placed = clip.Placement
		}
	}
	if placed == nil || placed.OpacityBasisPoints != 6_000 || placed.ScaleX != placement.ScaleX {
		t.Fatalf("compile input dropped the placement: %+v", placed)
	}
}
