package renderengine

import (
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

// SettleManifest rebases an execution manifest onto the single output frame at
// outputFrame.
//
// The manifest is recompiled rather than edited. Its plan digest, budget, input
// list and resource list are all derived from the plan, so settling the plan and
// patching the fields around it is how a manifest stops describing its own plan.
// Recompiling from the settled plan makes that impossible: the same compiler that
// produced the pinned manifest produces this one, and validates it.
//
// Material paths are carried over from the pinned manifest and narrowed to what
// the settled plan still references, because the compiler requires exactly one
// path per input and per font resource.
//
// ErrSettleInstantEmpty passes through: an instant with nothing active has no
// plan, and therefore no manifest.
func SettleManifest(
	manifest ExecutionManifest,
	outputFrame uint64,
) (ExecutionManifest, error) {
	if err := manifest.Validate(); err != nil {
		return ExecutionManifest{}, err
	}
	payload, err := SettlePlan(manifest.Plan, outputFrame)
	if err != nil {
		return ExecutionManifest{}, err
	}
	_, digest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, payload)
	if err != nil {
		return ExecutionManifest{}, err
	}
	tool := manifest.Tools[0]
	closure := ExecutionClosure{
		SHA256: manifest.CapabilityClosureSHA256,
		Tools:  map[string]ExecutionToolPin{tool.ID: {SHA256: tool.SHA256, Path: tool.Path}},
	}
	paths := MaterialPaths{
		ArtifactRoots: make(map[string]string, len(payload.Inputs)),
		Resources:     make(map[string]string, len(payload.FontResources)),
	}
	roots := make(map[string]string, len(manifest.Inputs))
	for _, input := range manifest.Inputs {
		roots[input.ArtifactID.String()] = input.ArtifactRoot
	}
	for _, input := range payload.Inputs {
		root, exists := roots[input.ArtifactID.String()]
		if !exists {
			return ExecutionManifest{}, fmt.Errorf("settle manifest input material is missing")
		}
		paths.ArtifactRoots[input.ArtifactID.String()] = root
	}
	resources := make(map[string]string, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		resources[resource.ID] = resource.Path
	}
	for _, resource := range payload.FontResources {
		path, exists := resources[resource.ResourceID]
		if !exists {
			return ExecutionManifest{}, fmt.Errorf("settle manifest resource material is missing")
		}
		paths.Resources[resource.ResourceID] = path
	}
	settled, _, err := CompileExecutionManifest(
		domain.RenderPlan{Payload: payload, Digest: digest},
		rendercontract.ExecutorIdentity{
			Version: manifest.RendererVersion, Target: manifest.RendererTarget,
		},
		closure,
		paths,
	)
	if err != nil {
		return ExecutionManifest{}, err
	}
	return settled, nil
}
