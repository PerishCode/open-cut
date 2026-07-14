package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const Schema = 1

type Runtime struct {
	Schema     int    `json:"schema"`
	Generation uint64 `json:"generation"`
	Active     string `json:"active,omitempty"`
	Candidate  string `json:"candidate,omitempty"`
	LastGood   string `json:"lastGood,omitempty"`
	Attempt    uint64 `json:"attempt"`
}

func Empty() Runtime { return Runtime{Schema: Schema} }

func Load(path, channel string) (Runtime, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Runtime{}, fmt.Errorf("read runtime state: %w", err)
	}
	var runtime Runtime
	if err := json.Unmarshal(data, &runtime); err != nil {
		return Runtime{}, fmt.Errorf("decode runtime state: %w", err)
	}
	if err := runtime.Validate(channel); err != nil {
		return Runtime{}, err
	}
	return runtime, nil
}

func Save(path, channel string, runtime Runtime) error {
	if err := runtime.Validate(channel); err != nil {
		return err
	}
	return atomicfile.WriteJSON(path, runtime, 0o600)
}

func (runtime Runtime) Validate(channel string) error {
	if runtime.Schema != Schema {
		return fmt.Errorf("unsupported runtime state schema %d", runtime.Schema)
	}
	for name, value := range map[string]string{
		"active": runtime.Active, "candidate": runtime.Candidate, "lastGood": runtime.LastGood,
	} {
		if value == "" {
			continue
		}
		if _, err := release.ParseVersionForChannel(value, channel); err != nil {
			return fmt.Errorf("invalid %s version: %w", name, err)
		}
	}
	if runtime.Candidate == "" && runtime.Active != runtime.LastGood {
		return fmt.Errorf("active must equal lastGood outside a candidate transition")
	}
	return nil
}

func Prepare(current Runtime, channel, candidate string) (Runtime, error) {
	if err := current.Validate(channel); err != nil {
		return Runtime{}, err
	}
	if current.Candidate != "" {
		return Runtime{}, fmt.Errorf("candidate %q is already pending", current.Candidate)
	}
	if _, err := release.ParseVersionForChannel(candidate, channel); err != nil {
		return Runtime{}, err
	}
	current.Generation++
	current.Attempt++
	current.Candidate = candidate
	return current, nil
}

func Confirm(current Runtime, channel, candidate string) (Runtime, error) {
	if err := current.Validate(channel); err != nil {
		return Runtime{}, err
	}
	if candidate == "" || current.Candidate != candidate {
		return Runtime{}, fmt.Errorf("cannot confirm %q while candidate is %q", candidate, current.Candidate)
	}
	current.Generation++
	current.Active = candidate
	current.LastGood = candidate
	current.Candidate = ""
	return current, nil
}

func Rollback(current Runtime, channel string) (Runtime, error) {
	if err := current.Validate(channel); err != nil {
		return Runtime{}, err
	}
	if current.Candidate == "" {
		return Runtime{}, fmt.Errorf("no candidate to roll back")
	}
	current.Generation++
	current.Candidate = ""
	current.Active = current.LastGood
	return current, nil
}
