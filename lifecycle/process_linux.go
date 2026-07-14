//go:build linux

package lifecycle

func applyPlatformProcessPolicy(spec ProcessSpec) ProcessSpec {
	if spec.Profile == ProfileHarness && spec.Sandbox == SandboxChromium {
		arguments := append([]string(nil), spec.Args...)
		for _, argument := range []string{"--no-sandbox", "--ozone-platform=headless"} {
			if !containsArgument(arguments, argument) {
				arguments = append(arguments, argument)
			}
		}
		spec.Args = arguments
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
