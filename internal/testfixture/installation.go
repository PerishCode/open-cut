package testfixture

import "github.com/PerishCode/open-cut/sidecar/protocol"

func InstallationAssertion() protocol.InstallationAssertion {
	return protocol.InstallationAssertion{
		Schema: 1, InstallationID: "installation-test-fixture", Generation: 1,
		Keys: []protocol.InstallationPublicKey{{
			Role: "harness", Algorithm: protocol.InstallationKeyAlgorithmEd25519,
			PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		}},
	}
}
