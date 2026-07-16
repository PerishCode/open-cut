package businessacceptance

import "strings"

var safeActorEnvironment = map[string]bool{
	"HOME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "PATH": true,
	"PATHEXT": true, "SystemRoot": true, "TEMP": true, "TMP": true, "TMPDIR": true,
	"USERPROFILE": true, "WINDIR": true,
}

func ActorEnvironment(environment []string) []string {
	result := make([]string, 0, len(safeActorEnvironment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && safeActorEnvironment[name] {
			result = append(result, entry)
		}
	}
	return result
}
