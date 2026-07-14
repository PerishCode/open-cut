//go:build !linux

package lifecycle

func applyPlatformProcessPolicy(spec ProcessSpec) ProcessSpec { return spec }
