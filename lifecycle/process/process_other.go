//go:build !linux

package process

func applyPlatformProcessPolicy(spec ProcessSpec) ProcessSpec { return spec }
