package mediatoolchain

import (
	"context"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
)

// Pinned source acquisition and archive-safe extraction are shared mechanism,
// not media policy: every toolchain closure fetches pinned bytes and unpacks
// them under the same supply-chain rules. Keeping one implementation means a
// hardening fix cannot land in one closure and rot in another.

type archiveIgnoredLink = toolchainclosure.ArchiveIgnoredLink

func ensureSource(ctx context.Context, archive string, source SourceRecord) error {
	return toolchainclosure.EnsureSource(ctx, archive, source)
}

func sourceArchiveSuffix(sourceURL string) (string, error) {
	return toolchainclosure.SourceArchiveSuffix(sourceURL)
}

func extractSource(archive, destination, prefix, requiredFile string) (string, error) {
	return toolchainclosure.ExtractSource(archive, destination, prefix, requiredFile)
}

func extractSourceIgnoringLinks(
	archive, destination, prefix, requiredFile string,
	ignoredLinks []archiveIgnoredLink,
) (string, error) {
	return toolchainclosure.ExtractSourceIgnoringLinks(
		archive, destination, prefix, requiredFile, ignoredLinks,
	)
}

type archiveSelection = toolchainclosure.ArchiveSelection

func sourceArchivePath(root string, source SourceRecord) (string, error) {
	return toolchainclosure.SourceArchivePath(root, source)
}

func extractZipFiles(archive, destination string, selections []archiveSelection) error {
	return toolchainclosure.ExtractZipFiles(archive, destination, selections)
}
