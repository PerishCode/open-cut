package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReadAssetSourceMaterial(
	ctx context.Context,
	assetID domain.AssetID,
) (domain.SourceGrantSummary, []byte, error) {
	var installationID string
	err := repository.db.QueryRowContext(ctx, `
SELECT sg.installation_id
FROM assets a JOIN source_grants sg ON sg.id = a.source_grant_id
WHERE a.id = ? AND a.tombstoned = 0`, assetID.String()).Scan(&installationID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SourceGrantSummary{}, nil, application.ErrSourceGrantNotFound
	}
	if err != nil {
		return domain.SourceGrantSummary{}, nil, err
	}
	var material []byte
	var grantValue string
	err = repository.db.QueryRowContext(ctx, `
SELECT sg.id, sg.protected_material
FROM assets a JOIN source_grants sg ON sg.id = a.source_grant_id
WHERE a.id = ? AND a.tombstoned = 0 AND sg.installation_id = ? AND sg.state = 'active'`,
		assetID.String(), installationID).Scan(&grantValue, &material)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SourceGrantSummary{}, nil, application.ErrSourceGrantNotFound
	}
	if err != nil {
		return domain.SourceGrantSummary{}, nil, err
	}
	grantID, err := domain.ParseSourceGrantID(grantValue)
	if err != nil {
		return domain.SourceGrantSummary{}, nil, err
	}
	grant, err := repository.ReadSourceGrant(ctx, installationID, grantID)
	if err != nil {
		return domain.SourceGrantSummary{}, nil, err
	}
	return grant, append([]byte(nil), material...), nil
}
