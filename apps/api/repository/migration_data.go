package repository

import (
	"context"
	"database/sql"

	"github.com/PerishCode/open-cut/internal/editmigration"
)

func applyDataMigration(ctx context.Context, tx *sql.Tx, version int) error {
	if version == 24 {
		return editmigration.RewriteUnifiedAlignmentJournals(ctx, tx)
	}
	if version == 25 {
		return editmigration.RewriteEditJournalSchemaV2ToV3(ctx, tx)
	}
	if version == 26 {
		return editmigration.RewriteEditJournalSchemaV3ToV4(ctx, tx)
	}
	if version == 27 {
		return editmigration.RewriteEditJournalSchemaV4ToV5(ctx, tx)
	}
	return nil
}
