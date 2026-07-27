package client

import "strings"

// CellStampArgumentPrefix marks a spawned suite member's argv with its cell
// stamp. The stamp is inert launch metadata: entries must accept and ignore
// it, and nothing may parse semantics out of it beyond equality with the
// roster record.
const CellStampArgumentPrefix = "--oc-cell-stamp="

// StripCellStampArguments returns args without any cell-stamp markers so
// strict argument dispatchers keep their exact shapes.
func StripCellStampArguments(args []string) []string {
	stripped := make([]string, 0, len(args))
	for _, argument := range args {
		if strings.HasPrefix(argument, CellStampArgumentPrefix) {
			continue
		}
		stripped = append(stripped, argument)
	}
	return stripped
}
