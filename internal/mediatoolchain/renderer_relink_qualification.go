package mediatoolchain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

func qualifyModifiedRendererRelink(
	ctx context.Context,
	kitRoot, sourceRoot, nativeRoot string,
	buildTarget target.Target,
	baselineDigest string,
	smoke RendererSmokeInput,
	stdout, stderr io.Writer,
) (string, uint64, string, RendererSmokeObservation, error) {
	qualificationRoot := filepath.Join(kitRoot, "qualification")
	if err := os.RemoveAll(qualificationRoot); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	if err := os.MkdirAll(qualificationRoot, 0o700); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	modifiedNative := filepath.Join(qualificationRoot, "native")
	for _, directory := range []string{"include", "lib"} {
		if err := copyRendererTree(
			filepath.Join(nativeRoot, directory), filepath.Join(modifiedNative, directory), nil,
		); err != nil {
			return "", 0, "", RendererSmokeObservation{}, err
		}
	}
	archive := filepath.Join(
		nativeRoot, "source", "fribidi-"+FriBidiSourceVersion+".tar.xz",
	)
	modifiedSource, err := extractSource(
		archive, filepath.Join(qualificationRoot, "source"),
		"fribidi-"+FriBidiSourceVersion, "configure",
	)
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	if err := markModifiedFriBidi(modifiedSource); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	modifiedSourceDigest, _, err := digestFile(filepath.Join(modifiedSource, "lib", "fribidi.c"))
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	compiler, err := tool.Resolve("cc")
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	shell, err := tool.Resolve("sh")
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	makeTool, err := tool.Resolve("make")
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	parallelism := runtime.NumCPU()
	if parallelism < 1 {
		parallelism = 1
	}
	if parallelism > 16 {
		parallelism = 16
	}
	if _, err := buildFriBidi(
		ctx, modifiedSource, modifiedNative, compiler, shell, makeTool, parallelism, stdout, stderr,
	); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	originalFriBidi, _, err := digestFile(filepath.Join(nativeRoot, "lib", "libfribidi.a"))
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	modifiedFriBidi, _, err := digestFile(filepath.Join(modifiedNative, "lib", "libfribidi.a"))
	if err != nil || modifiedFriBidi == originalFriBidi {
		return "", 0, "", RendererSmokeObservation{}, fmt.Errorf("modified FriBidi archive is unchanged")
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	output := filepath.Join(qualificationRoot, buildTarget.ExecutableName("open-cut-render-modified-relink"))
	if err := buildRendererFromRelinkKit(
		ctx, goTool, sourceRoot, modifiedNative, output, buildTarget, stdout, stderr,
	); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	digest, size, err := digestFile(output)
	if err != nil || digest == baselineDigest {
		return "", 0, "", RendererSmokeObservation{}, fmt.Errorf("modified-library renderer relink is unchanged")
	}
	observation, err := runRendererHelperSmoke(
		ctx, output, qualificationRoot, buildTarget, smoke,
	)
	if err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	if err := os.Remove(output); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	if err := os.RemoveAll(filepath.Join(qualificationRoot, "source")); err != nil {
		return "", 0, "", RendererSmokeObservation{}, err
	}
	return digest, size, modifiedSourceDigest, observation, nil
}

func markModifiedFriBidi(sourceRoot string) error {
	filename := filepath.Join(sourceRoot, "lib", "fribidi.c")
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	declaration := []byte("#include <fribidi.h>\n")
	if !bytes.Contains(data, declaration) {
		return fmt.Errorf("FriBidi relink marker declaration point is unavailable")
	}
	data = bytes.Replace(data, declaration, append(declaration,
		[]byte("\nstatic const volatile unsigned char open_cut_relink_smoke_marker = 1;\n")...), 1)
	probe := []byte("  DBG (\"in fribidi_log2vis\");\n")
	if !bytes.Contains(data, probe) {
		return fmt.Errorf("FriBidi relink marker probe point is unavailable")
	}
	data = bytes.Replace(data, probe, append(probe,
		[]byte("  if (open_cut_relink_smoke_marker == 0) return 0;\n")...), 1)
	return atomicfile.Write(filename, data, 0o600)
}
