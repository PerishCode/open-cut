package protocolgen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/tool"
)

type Mode string

const (
	ModeGenerate Mode = "generate"
	ModeCheck    Mode = "check"
)

type Result struct {
	Schema int      `json:"schema"`
	Mode   Mode     `json:"mode"`
	Files  []string `json:"files"`
}

func Run(ctx context.Context, repositoryRoot string, mode Mode, stdout, stderr io.Writer) (Result, error) {
	if mode != ModeGenerate && mode != ModeCheck {
		return Result{}, fmt.Errorf("unsupported protocol generation mode %q", mode)
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("resolve repository root: %w", err)
	}
	source := filepath.Join(root, "protocol", "sidecar", "v1", "main.tsp")
	if info, statErr := os.Stat(source); statErr != nil || !info.Mode().IsRegular() {
		return Result{}, fmt.Errorf("protocol source is missing at %s", source)
	}
	tempParent := filepath.Join(root, ".tmp", "oc-control")
	if err := os.MkdirAll(tempParent, 0o700); err != nil {
		return Result{}, fmt.Errorf("create protocol generation workspace: %w", err)
	}
	outputRoot, err := os.MkdirTemp(tempParent, "protocol-")
	if err != nil {
		return Result{}, fmt.Errorf("create protocol generation workspace: %w", err)
	}
	defer os.RemoveAll(outputRoot)

	pnpm, err := tool.Resolve("pnpm")
	if err != nil {
		return Result{}, err
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: pnpm,
		Args:       []string{"exec", "tsp", "compile", filepath.Dir(source), "--output-dir", outputRoot},
		Directory:  root,
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    lifecycle.ProfileDevelopment,
	}); err != nil {
		return Result{}, fmt.Errorf("compile TypeSpec protocol: %w", err)
	}

	openAPIJSON, err := os.ReadFile(filepath.Join(outputRoot, "openapi.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read generated OpenAPI JSON: %w", err)
	}
	document, err := parseOpenAPI(openAPIJSON)
	if err != nil {
		return Result{}, err
	}
	goBindings, err := generateGo(document)
	if err != nil {
		return Result{}, err
	}
	typeScriptBindings, err := generateTypeScript(document)
	if err != nil {
		return Result{}, err
	}
	openAPIYAML, err := os.ReadFile(filepath.Join(outputRoot, "openapi.yaml"))
	if err != nil {
		return Result{}, fmt.Errorf("read generated OpenAPI YAML: %w", err)
	}
	eventSchema, err := os.ReadFile(filepath.Join(outputRoot, "events.schema.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read generated event schema: %w", err)
	}
	eventSchema, err = normalizeEventSchema(eventSchema)
	if err != nil {
		return Result{}, err
	}

	outputs := map[string][]byte{
		filepath.Join("protocol", "sidecar", "v1", "openapi.yaml"):           openAPIYAML,
		filepath.Join("protocol", "sidecar", "v1", "events.schema.json"):     eventSchema,
		filepath.Join("sidecar", "protocol", "zz_generated.go"):              goBindings,
		filepath.Join("packages", "sidecar-protocol", "src", "generated.ts"): typeScriptBindings,
	}
	files := make([]string, 0, len(outputs))
	for relative := range outputs {
		files = append(files, filepath.ToSlash(relative))
	}
	sort.Strings(files)
	result := Result{Schema: 1, Mode: mode, Files: files}
	if mode == ModeCheck {
		var stale []string
		for relative, expected := range outputs {
			actual, readErr := os.ReadFile(filepath.Join(root, relative))
			if readErr != nil || !bytes.Equal(actual, expected) {
				stale = append(stale, filepath.ToSlash(relative))
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			return result, fmt.Errorf("generated protocol artifacts are stale: %v", stale)
		}
		return result, nil
	}
	for relative, data := range outputs {
		if err := atomicfile.Write(filepath.Join(root, relative), data, 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", filepath.ToSlash(relative), err)
		}
	}
	return result, nil
}
