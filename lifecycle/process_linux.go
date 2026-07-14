//go:build linux

package lifecycle

func applyPlatformProcessPolicy(spec ProcessSpec) ProcessSpec {
	if spec.Profile == ProfileHarness && spec.Sandbox == SandboxChromium && !containsArgument(spec.Args, "--no-sandbox") {
		spec.Args = append(append([]string(nil), spec.Args...), "--no-sandbox")
	}
	return spec
}

func containsArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}
