package toolchainclosure

// The Go closure inspection helpers live with the fingerprint that uses them.

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
)

const maximumListBytes = 32 << 20

type goPackage struct {
	Dir        string
	ImportPath string
	Standard   bool
	GoFiles    []string
	CgoFiles   []string
	CFiles     []string
	CXXFiles   []string
	HFiles     []string
	SFiles     []string
	EmbedFiles []string
	Module     *goModule
}

type boundedBuffer struct {
	bytes.Buffer
	Limit    int
	Exceeded bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	if buffer.Exceeded {
		return len(value), nil
	}
	remaining := buffer.Limit - buffer.Len()
	if len(value) > remaining {
		buffer.Exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(value[:remaining])
		}
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}

func goToolVersion(ctx context.Context, executable string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executable, Args: []string{"version"}, Stdout: &stdout, Stderr: &stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil || stdout.Len() == 0 || stdout.Len() > 16<<10 || stderr.Len() > 16<<10 {
		return "", fmt.Errorf("inspect renderer Go toolchain")
	}
	return strings.TrimSpace(stdout.String()), nil
}

type goModule struct {
	Path      string
	Version   string
	Dir       string
	GoMod     string
	GoVersion string
	Sum       string
	GoModSum  string
	Main      bool
	Replace   *goModule
}

// GoPackage and GoModule are exported so a caller can do its own analysis over
// the same closure the fingerprint was taken from, rather than re-asking the
// toolchain and risking a different answer.
type (
	GoPackage = goPackage
	GoModule  = goModule
)

// ListGoClosure returns the resolved package closure for a build target.
func ListGoClosure(
	ctx context.Context, repositoryRoot, tags, packagePath string,
) ([]GoPackage, error) {
	return fingerprintPackages(ctx, repositoryRoot, tags, packagePath)
}

// GoToolVersion identifies the compiler a closure was resolved with.
func GoToolVersion(ctx context.Context, executable string) (string, error) {
	return goToolVersion(ctx, executable)
}

// BoundedBuffer collects bounded subprocess output. Exported because the
// callers that inspect a Go closure need the same bound on what a toolchain
// may print at them.
type BoundedBuffer = boundedBuffer
