package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/state"
)

const journalSchema = 1

type journal struct {
	Schema        int       `json:"schema"`
	TransactionID string    `json:"transactionId"`
	Channel       string    `json:"channel"`
	Version       string    `json:"version"`
	SHA256        string    `json:"sha256"`
	Phase         string    `json:"phase"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func saveJournal(path string, value journal) error {
	value.Schema = journalSchema
	value.UpdatedAt = time.Now().UTC()
	return atomicfile.WriteJSON(path, value, 0o600)
}

func loadJournal(path string) (journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return journal{}, err
	}
	var value journal
	if err := json.Unmarshal(data, &value); err != nil {
		return journal{}, fmt.Errorf("decode update journal: %w", err)
	}
	if value.Schema != journalSchema || value.TransactionID == "" || strings.ContainsAny(value.TransactionID, `/\\`) || value.SHA256 == "" {
		return journal{}, fmt.Errorf("invalid update journal")
	}
	if _, err := release.ParseVersionForChannel(value.Version, value.Channel); err != nil {
		return journal{}, fmt.Errorf("invalid update journal version: %w", err)
	}
	return value, nil
}

func (installer Installer) Recover(bootstrap config.Bootstrap, paths layout.CellPaths) error {
	entry, err := loadJournal(paths.UpdateJournal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if entry.Channel != bootstrap.Channel {
		return fmt.Errorf("update journal channel does not match cell")
	}
	transactionRoot := filepath.Join(paths.Incoming, entry.TransactionID)
	installedRoot := filepath.Join(paths.Versions, entry.Version)
	runtimeState, err := state.Load(paths.StateFile, bootstrap.Channel)
	if err != nil {
		return err
	}
	if runtimeState.Active == entry.Version || runtimeState.Candidate == entry.Version {
		return finishRecovery(paths.UpdateJournal, transactionRoot)
	}
	if runtimeState.Candidate != "" {
		return fmt.Errorf("cannot recover %s while candidate %s is pending", entry.Version, runtimeState.Candidate)
	}
	if _, err := os.Stat(installedRoot); errors.Is(err, os.ErrNotExist) {
		return finishRecovery(paths.UpdateJournal, transactionRoot)
	} else if err != nil {
		return err
	}
	manifest, err := release.LoadManifest(filepath.Join(installedRoot, "manifest.json"))
	if err != nil {
		return fmt.Errorf("recover promoted release: %w", err)
	}
	if manifest.Version != entry.Version {
		return fmt.Errorf("recovered release manifest version mismatch")
	}
	if err := manifest.ValidateHost(bootstrap.Channel, bootstrap.ProtocolFloor); err != nil {
		return err
	}
	prepared, err := state.Prepare(runtimeState, bootstrap.Channel, entry.Version)
	if err != nil {
		return err
	}
	if err := state.Save(paths.StateFile, bootstrap.Channel, prepared); err != nil {
		return err
	}
	return finishRecovery(paths.UpdateJournal, transactionRoot)
}

func finishRecovery(journalPath, transactionRoot string) error {
	if err := os.RemoveAll(transactionRoot); err != nil {
		return err
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
