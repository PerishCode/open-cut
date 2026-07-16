package renderengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	ExecutionSchema       = 5
	MaximumExecutionBytes = 64 << 20
	ExecutionFilename     = "execution.json"
)

type ExecutionManifest struct {
	Schema                  int                      `json:"schema"`
	RendererVersion         string                   `json:"rendererVersion"`
	RendererTarget          string                   `json:"rendererTarget"`
	CapabilityClosureSHA256 domain.Digest            `json:"capabilityClosureSha256"`
	PlanDigest              domain.Digest            `json:"planDigest"`
	Plan                    domain.RenderPlanPayload `json:"plan"`
	Tools                   []ExecutionTool          `json:"tools"`
	Inputs                  []ExecutionInput         `json:"inputs"`
	Resources               []ExecutionResource      `json:"resources"`
	Budget                  ExecutionBudget          `json:"budget"`
	Output                  ExecutionOutput          `json:"output"`
	Result                  ExecutionResult          `json:"result"`
}

type ExecutionTool struct {
	ID     string        `json:"id"`
	Role   string        `json:"role"`
	SHA256 domain.Digest `json:"sha256"`
	Path   string        `json:"path"`
}

type ExecutionInput struct {
	ArtifactID        domain.ArtifactID `json:"artifactId"`
	ArtifactDigest    domain.Digest     `json:"artifactDigest"`
	ArtifactRoot      string            `json:"artifactRoot"`
	MediaRelativePath string            `json:"mediaRelativePath"`
}

type ExecutionResource struct {
	Kind    string        `json:"kind"`
	ID      string        `json:"id"`
	Version string        `json:"version"`
	SHA256  domain.Digest `json:"sha256"`
	Path    string        `json:"path"`
}

type ExecutionOutput struct {
	RelativePath string `json:"relativePath"`
}

type ExecutionResult struct {
	RelativePath string `json:"relativePath"`
}

type MaterialPaths struct {
	ArtifactRoots map[string]string
	Resources     map[string]string
}

type ExecutionToolPin struct {
	Path   string
	SHA256 domain.Digest
}

type ExecutionClosure struct {
	SHA256 domain.Digest
	Tools  map[string]ExecutionToolPin
}

func CompileExecutionManifest(
	plan application.PublishedRenderPlan,
	renderer application.RenderExecutorIdentity,
	closure ExecutionClosure,
	paths MaterialPaths,
) (ExecutionManifest, []byte, error) {
	paths = NormalizeMaterialPaths(paths)
	if len(paths.ArtifactRoots) != len(plan.Plan.Payload.Inputs) ||
		len(paths.Resources) != len(plan.Plan.Payload.FontResources) ||
		len(closure.Tools) != 1 {
		return ExecutionManifest{}, nil, fmt.Errorf("render execution material set is invalid")
	}
	ffmpeg, exists := closure.Tools["ffmpeg"]
	if !exists {
		return ExecutionManifest{}, nil, fmt.Errorf("render execution tool set is invalid")
	}
	manifest := ExecutionManifest{
		Schema: ExecutionSchema, RendererVersion: renderer.Version, RendererTarget: renderer.Target,
		CapabilityClosureSHA256: closure.SHA256,
		PlanDigest:              plan.Plan.Digest, Plan: plan.Plan.Payload,
		Tools: []ExecutionTool{{
			ID: "ffmpeg", Role: "raw-decoder-encoder", SHA256: ffmpeg.SHA256,
			Path: normalizeMaterialPath(ffmpeg.Path),
		}},
		Inputs:    make([]ExecutionInput, 0, len(plan.Plan.Payload.Inputs)),
		Resources: make([]ExecutionResource, 0, len(plan.Plan.Payload.FontResources)),
		Output:    ExecutionOutput{RelativePath: renderOutputRelativePath(plan.Plan.Payload.Purpose)},
		Result:    ExecutionResult{RelativePath: ResultFilename},
	}
	budget, err := CompileExecutionBudget(plan.Plan.Payload)
	if err != nil {
		return ExecutionManifest{}, nil, err
	}
	manifest.Budget = budget
	for _, input := range plan.Plan.Payload.Inputs {
		root, exists := paths.ArtifactRoots[input.ArtifactID.String()]
		if !exists {
			return ExecutionManifest{}, nil, fmt.Errorf("render execution input material is missing")
		}
		manifest.Inputs = append(manifest.Inputs, ExecutionInput{
			ArtifactID: input.ArtifactID, ArtifactDigest: input.ArtifactDigest,
			ArtifactRoot: root, MediaRelativePath: renderInputMediaRelativePath(input.Profile),
		})
	}
	for _, resource := range plan.Plan.Payload.FontResources {
		root, exists := paths.Resources[resource.ResourceID]
		if !exists {
			return ExecutionManifest{}, nil, fmt.Errorf("render execution resource material is missing")
		}
		manifest.Resources = append(manifest.Resources, ExecutionResource{
			Kind: "font-bundle", ID: resource.ResourceID, Version: resource.Version, SHA256: resource.SHA256,
			Path: root,
		})
	}
	if err := manifest.Validate(); err != nil {
		return ExecutionManifest{}, nil, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) == 0 || len(encoded) > MaximumExecutionBytes {
		return ExecutionManifest{}, nil, fmt.Errorf("render execution manifest exceeds its bound")
	}
	return manifest, encoded, nil
}

func DecodeExecutionManifest(data []byte) (ExecutionManifest, error) {
	if len(data) == 0 || len(data) > MaximumExecutionBytes {
		return ExecutionManifest{}, fmt.Errorf("render execution manifest size is invalid")
	}
	var manifest ExecutionManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		manifest.Validate() != nil {
		return ExecutionManifest{}, fmt.Errorf("render execution manifest is invalid")
	}
	return manifest, nil
}

func (manifest ExecutionManifest) Validate() error {
	if manifest.Schema != ExecutionSchema || manifest.RendererVersion == "" ||
		len(manifest.RendererVersion) > 1024 || validateTarget(manifest.RendererTarget) != nil ||
		manifest.CapabilityClosureSHA256 == "" || manifest.PlanDigest == "" ||
		manifest.Output.RelativePath != renderOutputRelativePath(manifest.Plan.Purpose) ||
		manifest.Result.RelativePath != ResultFilename ||
		len(manifest.Tools) != 1 ||
		len(manifest.Inputs) != len(manifest.Plan.Inputs) ||
		len(manifest.Resources) != len(manifest.Plan.FontResources) ||
		manifest.Budget.Validate(manifest.Plan) != nil {
		return fmt.Errorf("render execution manifest head is invalid")
	}
	_, digest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, manifest.Plan)
	if err != nil || digest != manifest.PlanDigest ||
		application.ValidateRenderPlanPayload(manifest.Plan) != nil {
		return fmt.Errorf("render execution plan digest is invalid")
	}
	tool := manifest.Tools[0]
	if tool.ID != "ffmpeg" || tool.Role != "raw-decoder-encoder" ||
		!validDigest(tool.SHA256) || !cleanAbsoluteRegular(tool.Path) {
		return fmt.Errorf("render execution tool closure is invalid")
	}
	previous := ""
	for index, input := range manifest.Inputs {
		planInput := manifest.Plan.Inputs[index]
		if input.ArtifactID != planInput.ArtifactID || input.ArtifactDigest != planInput.ArtifactDigest ||
			input.ArtifactID.String() <= previous || !cleanAbsoluteDirectory(input.ArtifactRoot) ||
			input.MediaRelativePath != renderInputMediaRelativePath(planInput.Profile) {
			return fmt.Errorf("render execution input is invalid")
		}
		previous = input.ArtifactID.String()
	}
	previous = ""
	for index, resource := range manifest.Resources {
		font := manifest.Plan.FontResources[index]
		if resource.Kind != "font-bundle" || resource.ID != font.ResourceID || resource.Version != font.Version ||
			resource.SHA256 != font.SHA256 || resource.ID <= previous || !cleanAbsoluteDirectory(resource.Path) {
			return fmt.Errorf("render execution resource is invalid")
		}
		previous = resource.ID
	}
	return nil
}

func renderOutputRelativePath(purpose domain.RenderPlanPurpose) string {
	switch purpose {
	case domain.RenderPurposeSequencePreview:
		return "preview.webm"
	case domain.RenderPurposeExport:
		return "export.webm"
	default:
		return ""
	}
}

func renderInputMediaRelativePath(profile string) string {
	switch profile {
	case application.SourceProxyProfile:
		return "proxy.webm"
	case application.RenderInputProfile:
		return "render-input.mkv"
	default:
		return ""
	}
}

func NormalizeMaterialPaths(paths MaterialPaths) MaterialPaths {
	result := MaterialPaths{
		ArtifactRoots: make(map[string]string, len(paths.ArtifactRoots)),
		Resources:     make(map[string]string, len(paths.Resources)),
	}
	for key, value := range paths.ArtifactRoots {
		result.ArtifactRoots[key] = normalizeMaterialPath(value)
	}
	for key, value := range paths.Resources {
		result.Resources[key] = normalizeMaterialPath(value)
	}
	return result
}

func normalizeMaterialPath(value string) string {
	cleaned := filepath.Clean(value)
	info, err := os.Lstat(cleaned)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return cleaned
	}
	physical, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return cleaned
	}
	return filepath.Clean(physical)
}

func validateTarget(value string) error {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return fmt.Errorf("render target is invalid")
	}
	_, err := target.New(parts[0], parts[1])
	return err
}

func cleanAbsoluteDirectory(value string) bool {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	physical, err := filepath.EvalSymlinks(value)
	if err != nil || filepath.Clean(physical) != value {
		return false
	}
	info, err := os.Lstat(value)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func cleanAbsoluteRegular(value string) bool {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	physical, err := filepath.EvalSymlinks(value)
	if err != nil || filepath.Clean(physical) != value {
		return false
	}
	info, err := os.Lstat(value)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func validDigest(value domain.Digest) bool {
	_, err := domain.ParseDigest(value.String())
	return err == nil
}
