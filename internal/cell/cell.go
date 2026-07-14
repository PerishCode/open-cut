package cell

import (
	"fmt"
	"path/filepath"
	"regexp"
)

const MaxSegmentLength = 64

var segmentPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)

type Identity struct {
	Channel   string `json:"channel"`
	Namespace string `json:"namespace"`
}

func New(channel, namespace string) (Identity, error) {
	identity := Identity{Channel: channel, Namespace: namespace}
	if err := identity.Validate(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (i Identity) Validate() error {
	if err := validateSegment("channel", i.Channel); err != nil {
		return err
	}
	if err := validateSegment("namespace", i.Namespace); err != nil {
		return err
	}
	return nil
}

func (i Identity) Suffix() string {
	return filepath.Join(i.Channel, i.Namespace)
}

func validateSegment(name, value string) error {
	if len(value) == 0 || len(value) > MaxSegmentLength || !segmentPattern.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase safe segment of 1-%d characters", name, MaxSegmentLength)
	}
	return nil
}
