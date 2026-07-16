package install

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/state"
)

func TestSignerTrustsEveryLifecycleSelectedReleaseDuringFirstActivation(t *testing.T) {
	root := t.TempDir()
	bootstrap := config.Bootstrap{
		Channel: "beta", Namespace: "signer-candidate",
		Roots: config.RootSet{
			BootstrapRoot: filepath.Join(root, "bootstrap"),
			StoreRoot:     filepath.Join(root, "store"),
			CacheRoot:     filepath.Join(root, "cache"),
			RuntimeRoot:   filepath.Join(root, "runtime"),
			LogRoot:       filepath.Join(root, "logs"),
		},
	}
	identity, err := cell.New(bootstrap.Channel, bootstrap.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(paths.StateFile, identity.Channel, state.Runtime{
		Schema: state.Schema, Generation: 1,
		Candidate: "0.1.0-beta.3", Active: "0.1.0-beta.2", LastGood: "0.1.0-beta.1",
	}); err != nil {
		t.Fatal(err)
	}
	roots, err := signerTrustedVersionRoots(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(paths.Versions, "0.1.0-beta.3"),
		filepath.Join(paths.Versions, "0.1.0-beta.2"),
		filepath.Join(paths.Versions, "0.1.0-beta.1"),
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("trusted roots=%v want=%v", roots, want)
	}
}
