package release

import (
	"fmt"
	"regexp"
	"strconv"
)

var canonicalVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)\.(0|[1-9][0-9]*)$`)

type Version struct {
	Major   uint64
	Minor   uint64
	Patch   uint64
	Channel string
	Number  uint64
	raw     string
}

func ParseVersion(value string) (Version, error) {
	matches := canonicalVersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return Version{}, fmt.Errorf("version %q must match X.Y.Z-<channel>.N", value)
	}
	parts := make([]uint64, 4)
	for index, source := range []string{matches[1], matches[2], matches[3], matches[5]} {
		parsed, err := strconv.ParseUint(source, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("parse version %q: %w", value, err)
		}
		parts[index] = parsed
	}
	return Version{
		Major: parts[0], Minor: parts[1], Patch: parts[2],
		Channel: matches[4], Number: parts[3], raw: value,
	}, nil
}

func ParseVersionForChannel(value, channel string) (Version, error) {
	version, err := ParseVersion(value)
	if err != nil {
		return Version{}, err
	}
	if version.Channel != channel {
		return Version{}, fmt.Errorf("version channel %q does not match cell channel %q", version.Channel, channel)
	}
	return version, nil
}

func (v Version) String() string { return v.raw }

func (v Version) Compare(other Version) int {
	left := []uint64{v.Major, v.Minor, v.Patch, v.Number}
	right := []uint64{other.Major, other.Minor, other.Patch, other.Number}
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	if v.Channel < other.Channel {
		return -1
	}
	if v.Channel > other.Channel {
		return 1
	}
	return 0
}

func (v Version) Display() string {
	if v.Channel == "stable" {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	}
	return v.raw
}
