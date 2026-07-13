package layout

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
)

type CellPaths struct {
	BootstrapRoot  string
	Store          string
	Cache          string
	Runtime        string
	Log            string
	CellFile       string
	StateFile      string
	UpdateJournal  string
	TrustRootFile  string
	Versions       string
	Incoming       string
	Downloads      string
	BrokerLock     string
	ControlFile    string
	OwnerTokenFile string
}

func Resolve(roots config.RootSet, identity cell.Identity) (CellPaths, error) {
	if err := roots.Validate(); err != nil {
		return CellPaths{}, err
	}
	if err := identity.Validate(); err != nil {
		return CellPaths{}, err
	}
	suffix := identity.Suffix()
	store := filepath.Join(roots.StoreRoot, suffix)
	cache := filepath.Join(roots.CacheRoot, suffix)
	runtimeRoot := filepath.Join(roots.RuntimeRoot, suffix)
	logRoot := filepath.Join(roots.LogRoot, suffix)
	return CellPaths{
		BootstrapRoot: roots.BootstrapRoot,
		Store:         store, Cache: cache, Runtime: runtimeRoot, Log: logRoot,
		CellFile:       filepath.Join(store, "cell.json"),
		StateFile:      filepath.Join(store, "state", "runtime.json"),
		UpdateJournal:  filepath.Join(store, "state", "update.json"),
		TrustRootFile:  filepath.Join(store, "trust", "root.json"),
		Versions:       filepath.Join(store, "versions"),
		Incoming:       filepath.Join(store, "incoming"),
		Downloads:      filepath.Join(cache, "downloads"),
		BrokerLock:     filepath.Join(runtimeRoot, "broker.lock"),
		ControlFile:    filepath.Join(runtimeRoot, "control.json"),
		OwnerTokenFile: filepath.Join(runtimeRoot, "owner.token"),
	}, nil
}

func (paths CellPaths) Ensure() error {
	directories := []struct {
		path string
		perm os.FileMode
	}{
		{filepath.Dir(paths.StateFile), 0o700},
		{filepath.Dir(paths.TrustRootFile), 0o700},
		{paths.Versions, 0o700},
		{paths.Incoming, 0o700},
		{paths.Downloads, 0o700},
		{paths.Runtime, 0o700},
		{paths.Log, 0o700},
	}
	for _, directory := range directories {
		if err := os.MkdirAll(directory.path, directory.perm); err != nil {
			return fmt.Errorf("create %s: %w", directory.path, err)
		}
	}
	return nil
}
