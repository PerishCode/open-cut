package environment

import (
	"sort"
	"strings"
)

// Merge applies removals and additions with case-insensitive variable identity,
// matching the strictest supported host semantics.
func Merge(base, unset []string, additions ...map[string]string) []string {
	removed := make(map[string]bool, len(unset))
	for _, name := range unset {
		removed[strings.ToUpper(name)] = true
	}
	values := make(map[string]string, len(base))
	for _, entry := range base {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		canonical := strings.ToUpper(name)
		if !removed[canonical] {
			values[canonical] = entry
		}
	}
	for _, addition := range additions {
		for name, value := range addition {
			canonical := strings.ToUpper(name)
			values[canonical] = name + "=" + value
		}
	}
	keys := make([]string, 0, len(values))
	for canonical := range values {
		keys = append(keys, canonical)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, canonical := range keys {
		result = append(result, values[canonical])
	}
	return result
}
