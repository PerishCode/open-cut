package mediatoolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

// cbuildStampName records what the preserved C build tree was produced from.
// The tree holds the static libraries and headers the renderer links against,
// plus the FFmpeg and Whisper executables. None of it depends on a single line
// of the renderer's own Go source, yet a renderer edit used to discard all of
// it: the build removed the tree unconditionally and spent six minutes
// recompiling byte-for-byte identical libraries.
const cbuildStampName = "c-build.stamp.json"

const cbuildStampSchema = 1

// cbuildStamp is deliberately small and exact. It is a reuse hint, never an
// authority: a stamp that matches only permits skipping work whose outputs are
// then checked for existence, and anything unexpected falls back to the full
// cold build rather than attempting a partial repair.
type cbuildStamp struct {
	Schema   int            `json:"schema"`
	Identity cbuildIdentity `json:"identity"`
	// The build derives these while compiling, and the manifest and recipe
	// digest need them afterwards. Reuse skips the compiling, so they are
	// carried here rather than recomputed - there is nothing left to recompute
	// them from.
	Configuration        []string              `json:"configuration"`
	LibVPXConfiguration  []string              `json:"libVpxConfiguration"`
	OpusConfiguration    []string              `json:"opusConfiguration"`
	WhisperConfiguration []string              `json:"whisperConfiguration"`
	NativeText           NativeTextBuildRecipe `json:"nativeText"`
	CompilerVersion      string                `json:"compilerVersion"`
}

// cbuildIdentity is the part a reuse decision compares. Everything in it is
// knowable before the build starts, which the recipe digest the manifest
// records is not - that digest only exists after the work it would let us
// skip.
type cbuildIdentity struct {
	ToolchainID     string `json:"toolchainId"`
	Version         string `json:"version"`
	Target          string `json:"target"`
	CompilerVersion string `json:"compilerVersion"`
	// BuildLogicSHA256 covers this package, which holds both the pinned catalog
	// and the code that turns it into a toolchain. The recipe digest the
	// manifest records would be the exact answer, but it is only computable
	// after the build it is meant to let us skip. Hashing the inputs instead
	// over-invalidates on an unrelated edit here and can never under-invalidate,
	// which is the direction a reuse hint has to err in.
	BuildLogicSHA256 string `json:"buildLogicSha256"`
}

func writeCBuildStamp(buildRoot string, stamp cbuildStamp) error {
	return atomicfile.WriteJSON(filepath.Join(buildRoot, cbuildStampName), stamp, 0o600)
}

// reusableCBuildTree reports whether the existing build tree was produced from
// exactly this recipe, and names the reason when it was not so a cold rebuild
// is never silent.
func reusableCBuildTree(buildRoot string, expected cbuildIdentity, outputs []string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(buildRoot, cbuildStampName))
	if err != nil {
		return false, "no preserved C build tree"
	}
	var recorded cbuildStamp
	if err := json.Unmarshal(raw, &recorded); err != nil {
		return false, "preserved C build stamp is unreadable"
	}
	if recorded.Schema != cbuildStampSchema || recorded.Identity != expected {
		return false, fmt.Sprintf(
			"preserved C build tree was produced from different inputs (recorded %s/%s, current %s/%s)",
			recorded.Identity.Version, shortRecipe(recorded.Identity.BuildLogicSHA256),
			expected.Version, shortRecipe(expected.BuildLogicSHA256),
		)
	}
	for _, output := range outputs {
		info, err := os.Stat(output)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false, fmt.Sprintf("preserved C build tree is missing %s", filepath.Base(output))
		}
	}
	return true, ""
}

func shortRecipe(digest string) string {
	if len(digest) > 16 {
		return digest[:16]
	}
	return digest
}

// cbuildReuseMaterial lists the files a preserved tree must still contain for a
// renderer relink to be possible: the executables the closure publishes and the
// static libraries the renderer links.
// cbuildReuseMaterial lists every file a later stage reads out of the build
// tree without recompiling it. It is deliberately exhaustive rather than
// representative: a missing entry here does not merely cost a rebuild, it lets
// reuse proceed and then fails much later in a stage that has no idea it was
// handed an incomplete tree. Checking the whole list up front turns that into a
// cold build with a reason.
func cbuildReuseMaterial(roots cbuildRoots, buildTarget target.Target) []string {
	material := []string{
		filepath.Join(roots.ffmpeg, buildTarget.ExecutableName("ffprobe")),
		filepath.Join(roots.ffmpeg, buildTarget.ExecutableName("ffmpeg")),
		roots.whisperBinary,
		filepath.Join(roots.dependency, "lib", "libfreetype.a"),
		filepath.Join(roots.dependency, "lib", "libfribidi.a"),
		filepath.Join(roots.dependency, "lib", "libharfbuzz.a"),
		filepath.Join(roots.harfBuzz, "src", "hb.h"),
		filepath.Join(roots.whisperSource, whisperConformanceModelSource),
	}
	// Licence and notice files are staged into the published closure, so a tree
	// that cannot supply them cannot be reused either.
	for _, notice := range []string{
		filepath.Join(roots.ffmpeg, "LICENSE.md"),
		filepath.Join(roots.ffmpeg, "COPYING.LGPLv2.1"),
		filepath.Join(roots.libVPX, "LICENSE"),
		filepath.Join(roots.libVPX, "PATENTS"),
		filepath.Join(roots.opus, "COPYING"),
		filepath.Join(roots.whisperSource, "LICENSE"),
		filepath.Join(roots.nativeText["freetype"], "LICENSE.TXT"),
		filepath.Join(roots.nativeText["fribidi"], "COPYING"),
		filepath.Join(roots.harfBuzz, "COPYING"),
	} {
		material = append(material, notice)
	}
	return material
}

// cbuildRoots names the directories a preserved tree is addressed through.
type cbuildRoots struct {
	ffmpeg        string
	libVPX        string
	opus          string
	harfBuzz      string
	dependency    string
	whisperSource string
	whisperBinary string
	nativeText    map[string]string
}

func readCBuildStamp(buildRoot string) (cbuildStamp, error) {
	raw, err := os.ReadFile(filepath.Join(buildRoot, cbuildStampName))
	if err != nil {
		return cbuildStamp{}, err
	}
	var stamp cbuildStamp
	if err := json.Unmarshal(raw, &stamp); err != nil {
		return cbuildStamp{}, err
	}
	return stamp, nil
}

// preservedWhisperBinary resolves the whisper executable a previous build left
// behind. CMake places it in one of two layouts, and an ambiguous tree is
// treated as no tree at all rather than guessed at.
func preservedWhisperBinary(buildRoot string, buildTarget target.Target) (string, error) {
	whisperBuildRoot := filepath.Join(buildRoot, "whisper")
	name := buildTarget.ExecutableName("whisper-cli")
	found := ""
	for _, candidate := range []string{
		filepath.Join(whisperBuildRoot, "bin", name),
		filepath.Join(whisperBuildRoot, "bin", "Release", name),
	} {
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("preserved whisper build is ambiguous")
		}
		found = candidate
	}
	if found == "" {
		return "", fmt.Errorf("no preserved whisper build")
	}
	return found, nil
}

func preservedWhisperSourceRoot(buildRoot string) string {
	return filepath.Join(buildRoot, "whisper.cpp-"+WhisperSourceVersion)
}

func preservedNativeTextRoots(buildRoot string) map[string]string {
	return map[string]string{
		"freetype": filepath.Join(buildRoot, "freetype-"+FreeTypeSourceVersion),
		"fribidi":  filepath.Join(buildRoot, "fribidi-"+FriBidiSourceVersion),
		"harfbuzz": filepath.Join(buildRoot, "harfbuzz-"+HarfBuzzSourceVersion),
	}
}
