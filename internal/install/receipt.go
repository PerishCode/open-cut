package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

const ReceiptSchema = 1

type IdentityBackend string

const (
	IdentityBackendDevelopmentFile IdentityBackend = "development-file"
	IdentityBackendPlatformSecure  IdentityBackend = "platform-secure"
)

type Receipt struct {
	Schema          int             `json:"schema"`
	Target          target.Target   `json:"target"`
	InstallRoot     string          `json:"installRoot"`
	HostPath        string          `json:"hostPath"`
	LauncherPath    string          `json:"launcherPath"`
	CLIPath         string          `json:"cliPath"`
	BootstrapPath   string          `json:"bootstrapPath"`
	ManagedRoots    []string        `json:"managedRoots"`
	Channel         string          `json:"channel"`
	Namespace       string          `json:"namespace"`
	HostPID         int             `json:"hostPid,omitempty"`
	IdentityBackend IdentityBackend `json:"identityBackend"`
}

func LoadReceipt(path string) (Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, err
	}
	if err := receipt.Validate(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func SaveReceipt(path string, receipt Receipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	return atomicfile.WriteJSON(path, receipt, 0o600)
}

func (receipt Receipt) Validate() error {
	if receipt.Schema != ReceiptSchema {
		return fmt.Errorf("unsupported install receipt schema %d", receipt.Schema)
	}
	if err := receipt.Target.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"installRoot": receipt.InstallRoot, "hostPath": receipt.HostPath,
		"launcherPath": receipt.LauncherPath, "cliPath": receipt.CLIPath, "bootstrapPath": receipt.BootstrapPath,
	} {
		if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return fmt.Errorf("%s must be a clean absolute path", name)
		}
	}
	if len(receipt.ManagedRoots) == 0 {
		return fmt.Errorf("install receipt requires managed roots")
	}
	if receipt.IdentityBackend != IdentityBackendDevelopmentFile && receipt.IdentityBackend != IdentityBackendPlatformSecure {
		return fmt.Errorf("install receipt requires a supported identity backend")
	}
	if receipt.Channel == "" || receipt.Namespace == "" || receipt.HostPID < 0 {
		return fmt.Errorf("install receipt requires channel, namespace, and a valid host PID")
	}
	for _, root := range receipt.ManagedRoots {
		if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return fmt.Errorf("managed roots must be clean absolute paths")
		}
	}
	return nil
}
