package cbuild

import (
	"fmt"

	"github.com/PerishCode/open-cut/utils/target"
)

func libVPXConfiguration(sourceRoot, prefix string, buildTarget target.Target) ([]string, error) {
	targetName, err := libVPXTarget(buildTarget)
	if err != nil {
		return nil, err
	}
	configuration := []string{
		"--prefix=" + prefix, "--target=" + targetName,
		"--disable-examples", "--disable-tools", "--disable-docs", "--disable-unit-tests",
		"--disable-vp8", "--disable-vp9-decoder", "--enable-vp9-encoder",
		"--disable-multithread", "--disable-runtime-cpu-detect", "--disable-vp9-highbitdepth",
		"--disable-webm-io", "--disable-libyuv", "--disable-shared", "--enable-static", "--enable-pic",
		"--extra-cflags=-ffile-prefix-map=" + sourceRoot + "=.",
	}
	switch buildTarget.Arch {
	case target.ARM64:
		configuration = append(configuration,
			"--disable-neon-dotprod", "--disable-neon-i8mm", "--disable-sve", "--disable-sve2",
		)
	case target.X64:
		configuration = append(configuration,
			"--disable-sse3", "--disable-ssse3", "--disable-sse4-1",
			"--disable-avx", "--disable-avx2", "--disable-avx512",
		)
	default:
		return nil, fmt.Errorf("libvpx baseline architecture is unsupported")
	}
	return configuration, nil
}
