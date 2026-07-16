package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func loadMediaClaimStreams(
	ctx context.Context,
	tx *sql.Tx,
	assetID domain.AssetID,
	fingerprint domain.Digest,
) ([]domain.SourceStream, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, descriptor_json FROM source_streams
WHERE asset_id = ? AND fingerprint = ?
ORDER BY container_index`, assetID.String(), fingerprint.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	streams := make([]domain.SourceStream, 0)
	for rows.Next() {
		var idValue, descriptorJSON string
		if err := rows.Scan(&idValue, &descriptorJSON); err != nil {
			return nil, err
		}
		id, parseErr := domain.ParseSourceStreamID(idValue)
		if parseErr != nil {
			return nil, application.ErrAssetInvalid
		}
		var descriptor domain.SourceStreamDescriptor
		if err := json.Unmarshal([]byte(descriptorJSON), &descriptor); err != nil || descriptor.Validate() != nil {
			return nil, application.ErrAssetInvalid
		}
		streams = append(streams, domain.SourceStream{ID: id, Descriptor: descriptor})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(streams) == 0 {
		return nil, application.ErrAssetInvalid
	}
	return streams, nil
}
