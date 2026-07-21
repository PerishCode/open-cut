package cbuild

// These exercise the C configure lines themselves, so they live with the code
// that produces them rather than with the manifest that records them.

import (
	"slices"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/utils/target"
)

func TestBuildConfigurationNormalizationRemovesEphemeralPaths(t *testing.T) {
	configuration := []string{
		"--cc=/opt/toolchain/bin/cc",
		"--extra-cflags=-I/work/build/dependencies/include -ffile-prefix-map=/work/build=.",
		"--extra-ldflags=-L/work/build/dependencies/lib",
	}
	normalized := normalizeBuildConfiguration(
		configuration, "/work/build", "/work/build/dependencies", "/opt/toolchain/bin/cc",
	)
	want := []string{
		"--cc=$cc",
		"--extra-cflags=-I$deps/include -ffile-prefix-map=$build=.",
		"--extra-ldflags=-L$deps/lib",
	}
	if strings.Join(normalized, "\n") != strings.Join(want, "\n") {
		t.Fatalf("normalized=%q", normalized)
	}
}

func TestWindowsFFmpegConfigurationLinksToolchainRuntimeStatically(t *testing.T) {
	windows := buildConfiguration(
		"$cc", "$build", "$deps", target.Target{Platform: target.Win, Arch: target.X64},
	)
	if !ValidLGPLConfiguration(windows) ||
		!slices.Contains(windows, "--extra-ldflags=-L$deps/lib -static") {
		t.Fatalf("windows configuration=%q", windows)
	}
	mac := buildConfiguration(
		"$cc", "$build", "$deps", target.Target{Platform: target.Mac, Arch: target.ARM64},
	)
	if slices.Contains(mac, "--extra-ldflags=-L$deps/lib -static") {
		t.Fatalf("mac configuration=%q", mac)
	}
}

func TestLibVPXConfigurationPinsBaselineCPUFeatures(t *testing.T) {
	for _, fixture := range []struct {
		buildTarget target.Target
		disabled    []string
	}{
		{
			buildTarget: target.Target{Platform: target.Mac, Arch: target.ARM64},
			disabled: []string{
				"--disable-neon-dotprod", "--disable-neon-i8mm", "--disable-sve", "--disable-sve2",
			},
		},
		{
			buildTarget: target.Target{Platform: target.Win, Arch: target.X64},
			disabled: []string{
				"--disable-sse3", "--disable-ssse3", "--disable-sse4-1",
				"--disable-avx", "--disable-avx2", "--disable-avx512",
			},
		},
	} {
		configuration, err := libVPXConfiguration("/source", "/prefix", fixture.buildTarget)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(configuration, "--disable-runtime-cpu-detect") ||
			slices.Contains(configuration, "--enable-runtime-cpu-detect") {
			t.Fatalf("target=%s configuration=%q", fixture.buildTarget, configuration)
		}
		for _, required := range fixture.disabled {
			if !slices.Contains(configuration, required) {
				t.Fatalf("target=%s missing=%s configuration=%q", fixture.buildTarget, required, configuration)
			}
		}
	}
}

func TestOpusConfigurationPinsFixedPointBaseline(t *testing.T) {
	configuration := opusConfiguration("/source", "/prefix")
	for _, required := range []string{
		"--enable-fixed-point", "--disable-asm", "--disable-rtcd", "--disable-intrinsics",
	} {
		if !slices.Contains(configuration, required) {
			t.Fatalf("missing=%s configuration=%q", required, configuration)
		}
	}
	if slices.Contains(configuration, "--disable-float-api") {
		t.Fatalf("FFmpeg's libopus wrapper still requires the link-time float API")
	}
}
