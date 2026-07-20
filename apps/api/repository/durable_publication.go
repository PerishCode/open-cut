package repository

import (
	"database/sql"
	"errors"
	"os"
)

// A published artifact is durable in two places: an immutable tree renamed
// into its canonical root, and the SQLite row that makes the tree visible.
// The rename happens first, so every publisher must decide what to do with
// those bytes when the commit that would publish them fails.
//
// A commit failure is ambiguous: the transaction may already be durable when
// the error surfaces. Preserving the tree is self-healing in both directions —
// cold-start reconciliation removes any tree whose record was never committed,
// while deleting bytes for a commit that did succeed leaves a ready record
// pointing at nothing, which no reconciliation can repair. Publishers
// therefore keep published bytes whenever the commit outcome is unknown, and
// remove them only for failures that provably happened before the commit.
type ambiguousCommitError struct{ cause error }

func (err ambiguousCommitError) Error() string { return err.cause.Error() }
func (err ambiguousCommitError) Unwrap() error { return err.cause }

// commitPublication commits the transaction that publishes an already-renamed
// artifact tree, marking a commit failure ambiguous so the caller's cleanup
// preserves the published bytes.
func commitPublication(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return ambiguousCommitError{cause: err}
	}
	return nil
}

// discardUnpublishedTree removes an artifact tree that was renamed into place
// for a publication that provably never committed. It is the deferred cleanup
// every publisher shares: deriving the decision from the returned error keeps
// the durability policy in one place instead of a flag each call site must
// remember to set before every return.
//
// Integration tests cannot reach the ambiguous branch, because they cannot
// make a commit fail after it is durable. Having exactly one implementation
// is therefore the protection: do not reduce this to an unconditional
// removal, and do not reintroduce per-publisher cleanup.
func discardUnpublishedTree(root string, resultErr *error) {
	if *resultErr == nil {
		return
	}
	var ambiguous ambiguousCommitError
	if errors.As(*resultErr, &ambiguous) {
		return
	}
	_ = os.RemoveAll(root)
}
