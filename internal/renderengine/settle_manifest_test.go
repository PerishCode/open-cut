package renderengine

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/lifecycle/process"
)

func settleTraversal(t *testing.T, manifest ExecutionManifest, attemptRoot string) []byte {
	t.Helper()
	producer, err := newVideoStreamProducer(
		manifest, attemptRoot, process.ProfileHarness, (&fakeVideoRunFactory{}).start,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := producer(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// This is what the whole rebase is for. A settled manifest must produce exactly
// the frame the ordinary full traversal produces at that ordinal — not a similar
// frame, the same bytes. Anything less and a settling gesture would show the
// creator something the commit would not reproduce.
func TestSettledManifestProducesTheSameBytesAsTheFullTraversal(t *testing.T) {
	fixture := newVideoEvaluatorFixture(t, executionClosure(t))
	full := settleTraversal(t, fixture.manifest, fixture.attemptRoot)
	size, err := rawYUVFrameBytes(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	frameBytes := uint64(size)
	frameCount := fixture.manifest.Plan.Output.VideoFrameCount.Value()
	if uint64(len(full)) != frameBytes*frameCount {
		t.Fatalf("full traversal produced %d bytes for %d frames", len(full), frameCount)
	}
	for _, outputFrame := range []uint64{0, 1, 2, 15, 28, 29} {
		settled, err := SettleManifest(fixture.manifest, outputFrame)
		if errors.Is(err, ErrSettleInstantEmpty) {
			continue
		}
		if err != nil {
			t.Fatalf("frame %d: %v", outputFrame, err)
		}
		one := settleTraversal(t, settled, fixture.attemptRoot)
		if uint64(len(one)) != frameBytes {
			t.Fatalf("frame %d: settled traversal produced %d bytes", outputFrame, len(one))
		}
		expected := full[outputFrame*frameBytes : (outputFrame+1)*frameBytes]
		if !bytes.Equal(one, expected) {
			t.Fatalf("frame %d: the settled frame is not the frame the traversal produces", outputFrame)
		}
	}
}

// The manifest's plan digest, budget, inputs and resources are all derived from
// its plan. Recompiling is what keeps them describing the settled plan rather
// than the pinned one.
func TestSettledManifestDescribesItsOwnPlan(t *testing.T) {
	fixture := newVideoEvaluatorFixture(t, executionClosure(t))
	settled, err := SettleManifest(fixture.manifest, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := settled.Validate(); err != nil {
		t.Fatalf("settled manifest is invalid: %v", err)
	}
	if settled.PlanDigest == fixture.manifest.PlanDigest {
		t.Fatal("the settled manifest still carries the pinned plan's digest")
	}
	if len(settled.Inputs) != len(settled.Plan.Inputs) {
		t.Fatalf("inputs=%d plan inputs=%d", len(settled.Inputs), len(settled.Plan.Inputs))
	}
	if len(settled.Resources) != len(settled.Plan.FontResources) {
		t.Fatalf("resources=%d fonts=%d", len(settled.Resources), len(settled.Plan.FontResources))
	}
	if settled.RendererVersion != fixture.manifest.RendererVersion ||
		settled.RendererTarget != fixture.manifest.RendererTarget ||
		settled.CapabilityClosureSHA256 != fixture.manifest.CapabilityClosureSHA256 {
		t.Fatal("settling changed the renderer provenance")
	}
	if fixture.manifest.Plan.Output.VideoFrameCount.Value() == 1 {
		t.Fatal("the fixture cannot distinguish a settled manifest from its pinned one")
	}
}

func TestSettleManifestRejectsAFrameOutsideThePinnedOutput(t *testing.T) {
	fixture := newVideoEvaluatorFixture(t, executionClosure(t))
	frameCount := fixture.manifest.Plan.Output.VideoFrameCount.Value()
	if _, err := SettleManifest(fixture.manifest, frameCount); err == nil {
		t.Fatal("a frame past the pinned output grid was accepted")
	}
}
