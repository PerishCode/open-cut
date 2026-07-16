package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func ensureSourceStreamState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	assetID domain.AssetID,
	streamID domain.SourceStreamID,
) error {
	if current, exists := state.SourceStreams[streamID.String()]; exists {
		if current.AssetID != assetID {
			return application.ErrEditInvalid
		}
		return nil
	}
	var streamValue, assetValue, descriptorJSON string
	var assetRevision uint64
	err := tx.QueryRowContext(ctx, `
SELECT stream.id, stream.asset_id, stream.descriptor_json, asset.revision
FROM source_streams stream
JOIN assets asset ON asset.id = stream.asset_id
  AND asset.project_id = ? AND asset.tombstoned = 0
  AND asset.accepted_fingerprint = stream.fingerprint
WHERE stream.id = ? AND stream.asset_id = ?`,
		state.ProjectID.String(), streamID.String(), assetID.String(),
	).Scan(&streamValue, &assetValue, &descriptorJSON, &assetRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrEditInvalid
	}
	if err != nil {
		return err
	}
	parsedStream, err := domain.ParseSourceStreamID(streamValue)
	if err != nil {
		return err
	}
	parsedAsset, err := domain.ParseAssetID(assetValue)
	if err != nil {
		return err
	}
	revision, err := domain.NewRevision(assetRevision)
	if err != nil {
		return err
	}
	var descriptor domain.SourceStreamDescriptor
	if json.Unmarshal([]byte(descriptorJSON), &descriptor) != nil || descriptor.Validate() != nil {
		return application.ErrEditInvalid
	}
	state.SourceStreams[streamValue] = application.EditSourceStreamState{
		ID: parsedStream, AssetID: parsedAsset, AssetRevision: revision, Descriptor: descriptor,
	}
	return nil
}

func ensureLinkGroupState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.LinkGroupID,
) error {
	if _, exists := state.LinkGroups[id.String()]; exists {
		return nil
	}
	group, err := loadLinkGroupState(ctx, tx, state.ProjectID, id)
	if err != nil {
		return err
	}
	state.LinkGroups[id.String()] = group
	return nil
}

func loadLinkGroupMembers(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.LinkGroupID,
) error {
	if _, loaded := state.LinkGroupClips[id.String()]; loaded {
		return nil
	}
	if err := ensureLinkGroupState(ctx, tx, state, id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM clips
WHERE project_id = ? AND sequence_id = ? AND link_group_id = ? AND tombstoned = 0
ORDER BY id LIMIT 65`, state.ProjectID.String(), state.SequenceID.String(), id.String())
	if err != nil {
		return err
	}
	defer rows.Close()
	members := make([]domain.ClipID, 0, 2)
	for rows.Next() {
		if len(members) == 64 {
			return application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		memberID, err := domain.ParseClipID(value)
		if err != nil {
			return err
		}
		if err := ensureClipState(ctx, tx, state, memberID); err != nil {
			return err
		}
		members = append(members, memberID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	state.LinkGroupClips[id.String()] = members
	return nil
}

func loadLinkGroupState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	id domain.LinkGroupID,
) (domain.LinkGroupState, error) {
	var idValue, sequenceValue string
	var revisionValue uint64
	var tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT id, sequence_id, revision, tombstoned
FROM clip_link_groups WHERE id = ? AND project_id = ?`, id.String(), projectID.String()).Scan(
		&idValue, &sequenceValue, &revisionValue, &tombstoned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LinkGroupState{}, application.ErrEditInvalid
	}
	if err != nil {
		return domain.LinkGroupState{}, err
	}
	parsedID, err := domain.ParseLinkGroupID(idValue)
	if err != nil {
		return domain.LinkGroupState{}, err
	}
	sequenceID, err := domain.ParseSequenceID(sequenceValue)
	if err != nil {
		return domain.LinkGroupState{}, err
	}
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return domain.LinkGroupState{}, err
	}
	return domain.LinkGroupState{
		ID: parsedID, Revision: revision, SequenceID: sequenceID, Tombstoned: tombstoned,
	}, nil
}

func ensureClipState(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	id domain.ClipID,
) error {
	if _, exists := state.Clips[id.String()]; exists {
		return nil
	}
	clip, err := loadClipState(ctx, tx, state.ProjectID, id)
	if err != nil {
		return err
	}
	state.Clips[id.String()] = clip
	return nil
}

func loadClipState(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	id domain.ClipID,
) (domain.ClipState, error) {
	var idValue, sequenceValue, trackValue, assetValue, streamValue string
	var linkGroupValue sql.NullString
	var revisionValue uint64
	var sourceStart, sourceDuration, timelineStart, timelineDuration int64
	var sourceStartScale, sourceDurationScale, timelineStartScale, timelineDurationScale int32
	var enabled, tombstoned bool
	err := tx.QueryRowContext(ctx, `
SELECT id, sequence_id, track_id, asset_id, source_stream_id, revision,
       source_start_value, source_start_scale, source_duration_value, source_duration_scale,
       timeline_start_value, timeline_start_scale, timeline_duration_value, timeline_duration_scale,
       enabled, link_group_id, tombstoned
FROM clips WHERE id = ? AND project_id = ?`, id.String(), projectID.String()).Scan(
		&idValue, &sequenceValue, &trackValue, &assetValue, &streamValue, &revisionValue,
		&sourceStart, &sourceStartScale, &sourceDuration, &sourceDurationScale,
		&timelineStart, &timelineStartScale, &timelineDuration, &timelineDurationScale,
		&enabled, &linkGroupValue, &tombstoned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ClipState{}, application.ErrEditInvalid
	}
	if err != nil {
		return domain.ClipState{}, err
	}
	clipID, _ := domain.ParseClipID(idValue)
	sequenceID, _ := domain.ParseSequenceID(sequenceValue)
	trackID, _ := domain.ParseTrackID(trackValue)
	assetID, _ := domain.ParseAssetID(assetValue)
	streamID, _ := domain.ParseSourceStreamID(streamValue)
	revision, err := domain.NewRevision(revisionValue)
	if err != nil {
		return domain.ClipState{}, err
	}
	sourceRange, err := storedTimeRange(sourceStart, sourceStartScale, sourceDuration, sourceDurationScale)
	if err != nil {
		return domain.ClipState{}, err
	}
	timelineRange, err := storedTimeRange(timelineStart, timelineStartScale, timelineDuration, timelineDurationScale)
	if err != nil {
		return domain.ClipState{}, err
	}
	var linkGroupID *domain.LinkGroupID
	if linkGroupValue.Valid {
		parsed, parseErr := domain.ParseLinkGroupID(linkGroupValue.String)
		if parseErr != nil {
			return domain.ClipState{}, parseErr
		}
		linkGroupID = &parsed
	}
	return domain.ClipState{
		ID: clipID, Revision: revision, SequenceID: sequenceID, TrackID: trackID,
		AssetID: assetID, SourceStreamID: streamID, SourceRange: sourceRange,
		TimelineRange: timelineRange, Enabled: enabled, LinkGroupID: linkGroupID, Tombstoned: tombstoned,
	}, nil
}

func storedTimeRange(startValue int64, startScale int32, durationValue int64, durationScale int32) (domain.TimeRange, error) {
	start, err := domain.NewRationalTime(startValue, startScale)
	if err != nil {
		return domain.TimeRange{}, err
	}
	duration, err := domain.NewRationalTime(durationValue, durationScale)
	if err != nil {
		return domain.TimeRange{}, err
	}
	return domain.NewTimeRange(start, duration)
}

func loadClipOverlaps(
	ctx context.Context,
	tx *sql.Tx,
	state *application.EditNormalizationState,
	sequenceID domain.SequenceID,
	trackID domain.TrackID,
	rangeValue domain.TimeRange,
) error {
	startKey, err := domain.RationalOrderKey(rangeValue.Start)
	if err != nil {
		return application.ErrEditInvalid
	}
	end, err := rangeValue.End()
	if err != nil {
		return application.ErrEditInvalid
	}
	endKey, err := domain.RationalOrderKey(end)
	if err != nil {
		return application.ErrEditInvalid
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM clips
WHERE sequence_id = ? AND track_id = ? AND tombstoned = 0
  AND timeline_start_order_key < ? AND timeline_end_order_key > ?
ORDER BY timeline_start_order_key, id LIMIT 513`, sequenceID.String(), trackID.String(), endKey, startKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 512 {
			return application.ErrEditInvalid
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		id, err := domain.ParseClipID(value)
		if err != nil {
			return err
		}
		if err := ensureClipState(ctx, tx, state, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
