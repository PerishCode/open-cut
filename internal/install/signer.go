package install

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func RunSigner(
	ctx context.Context,
	receiptPath, executable string,
	stdin io.Reader,
	stdout io.Writer,
) error {
	if receiptPath == "" {
		receiptPath = lifecycle.DefaultReceiptPath(executable)
	}
	receipt, err := LoadReceipt(receiptPath)
	if err != nil {
		return fmt.Errorf("load signer receipt: %w", err)
	}
	decoder := json.NewDecoder(io.LimitReader(stdin, 256<<10))
	decoder.DisallowUnknownFields()
	var request lifecycle.SignerRequest
	if err := decoder.Decode(&request); err != nil || request.Schema != lifecycle.SignerRequestSchema {
		return fmt.Errorf("invalid platform signer request")
	}
	payload, err := base64.RawURLEncoding.DecodeString(request.Payload)
	if err != nil || len(payload) == 0 || len(payload) > 64<<10 {
		return fmt.Errorf("invalid platform signer payload")
	}
	bootstrap, err := config.LoadBootstrap(receipt.BootstrapPath)
	if err != nil {
		return err
	}
	trustedRoots, err := signerTrustedVersionRoots(bootstrap)
	if err != nil {
		return err
	}
	if err := verifyPlatformSignerCaller(ctx, request.Role, receipt, executable, trustedRoots); err != nil {
		return err
	}
	roles := make([]string, 0, len(bootstrap.Installation.Keys))
	for _, key := range bootstrap.Installation.Keys {
		roles = append(roles, key.Role)
	}
	sort.Strings(roles)
	if receipt.IdentityBackend != IdentityBackendDevelopmentFile {
		return fmt.Errorf("platform secure identity backend is unavailable")
	}
	identityRoot := filepath.Join(filepath.Dir(bootstrap.Roots.BootstrapRoot), "identity")
	identity, err := lifecycle.LoadDevelopmentInstallationIdentity(identityRoot, roles)
	if err != nil {
		return err
	}
	if !sameInstallationAssertion(identity.Assertion(), bootstrap.Installation) {
		return fmt.Errorf("signer identity does not match bootstrap installation assertion")
	}
	signature, err := identity.Sign(request.Role, payload)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(lifecycle.SignerResponse{
		Schema: lifecycle.SignerRequestSchema, InstallationID: bootstrap.Installation.InstallationID,
		InstallationGeneration: bootstrap.Installation.Generation, Role: request.Role,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	})
}

func signerTrustedVersionRoots(bootstrap config.Bootstrap) ([]string, error) {
	identity, err := cell.New(bootstrap.Channel, bootstrap.Namespace)
	if err != nil {
		return nil, err
	}
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return nil, err
	}
	runtimeState, err := state.Load(paths.StateFile, identity.Channel)
	if err != nil {
		return nil, err
	}
	versions := []string{runtimeState.Candidate, runtimeState.Active, runtimeState.LastGood}
	seen := make(map[string]bool, len(versions))
	roots := make([]string, 0, len(versions))
	for _, version := range versions {
		if version != "" && !seen[version] {
			seen[version] = true
			roots = append(roots, filepath.Join(paths.Versions, version))
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("platform signer requires a lifecycle-selected release")
	}
	return roots, nil
}

func sameInstallationAssertion(left, right protocol.InstallationAssertion) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
