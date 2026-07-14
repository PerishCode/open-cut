package lifecycle

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/PerishCode/open-cut/internal/cell"
)

var productDataSegment = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// ResolveProductDataDir absorbs platform-specific user-data selection into one
// stable, cell-scoped path. Installers choose basePath; runtime layers only see
// the returned directory.
func ResolveProductDataDir(basePath, productID, channel, namespace string) (string, error) {
	if !filepath.IsAbs(basePath) || filepath.Clean(basePath) != basePath {
		return "", fmt.Errorf("product data base path must be a clean absolute path")
	}
	if !productDataSegment.MatchString(productID) {
		return "", fmt.Errorf("product data identity must be a lowercase safe segment")
	}
	identity, err := cell.New(channel, namespace)
	if err != nil {
		return "", err
	}
	return filepath.Join(basePath, productID, identity.Suffix()), nil
}
