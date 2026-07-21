package cbuild

// The C toolchain speaks the shared closure vocabulary rather than defining its
// own: the records it produces are consumed verbatim by whichever manifest
// describes the artifacts it built.

import (
	"context"
	"encoding/json"
	"os"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
)

type (
	SourceRecord = toolchainclosure.SourceRecord
	NoticeRecord = toolchainclosure.NoticeRecord
)

func copyRegularFile(source, destination string, mode os.FileMode) error {
	return toolchainclosure.CopyRegularFile(source, destination, mode)
}

func digestFile(filename string) (string, uint64, error) {
	return toolchainclosure.DigestFile(filename)
}

func extractSource(archive, destination, prefix, requiredFile string) (string, error) {
	return toolchainclosure.ExtractSource(archive, destination, prefix, requiredFile)
}

func hashDirectories(roots ...string) (string, error) {
	return toolchainclosure.HashDirectories(roots...)
}

func verifyPackagedExecutableDynamicClosure(filename string) error {
	return toolchainclosure.VerifyPackagedExecutableDynamicClosure(filename)
}

type archiveIgnoredLink = toolchainclosure.ArchiveIgnoredLink

func extractSourceIgnoringLinks(
	archive, destination, prefix, requiredFile string, ignoredLinks []archiveIgnoredLink,
) (string, error) {
	return toolchainclosure.ExtractSourceIgnoringLinks(
		archive, destination, prefix, requiredFile, ignoredLinks,
	)
}

func validDigest(value string) bool { return toolchainclosure.ValidDigest(value) }

const (
	// buildLogicClosureDomain separates this group's closure identity from the
	// renderer's, so two groups can never produce the same fingerprint even if
	// they ever resolved to the same packages.
	buildLogicClosureDomain = "cbuild-source-closure-v1"
	buildLogicPackage       = "github.com/PerishCode/open-cut/internal/mediatoolchain/cbuild"
)

// SourceClosureFingerprint identifies the logic that compiles the C toolchain.
// A cache keyed by it survives every change that cannot reach the compiler.
func SourceClosureFingerprint(ctx context.Context, repositoryRoot string) (string, error) {
	return toolchainclosure.GoSourceClosureFingerprint(
		ctx, repositoryRoot, buildLogicClosureDomain, "", buildLogicPackage,
	)
}

// CatalogFingerprint identifies only what gets downloaded. Which archives to
// fetch is decided by the pins, so a build-logic change must not re-download
// most of a gigabyte of pinned sources that did not move.
func CatalogFingerprint(ctx context.Context, repositoryRoot string) (string, error) {
	records := append(SourceRecords(), NativeTextSourceRecords()...)
	encoded, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	return toolchainclosure.ClosureDigest("cbuild-source-catalog-v1", json.RawMessage(encoded))
}
