package command

import (
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type AssetListInput struct {
	After string `json:"after,omitempty" maxLength:"512" doc:"Opaque query-local continuation cursor"`
	Limit uint16 `json:"limit,omitempty" minimum:"1" maximum:"100" default:"50" doc:"Maximum Asset summaries to return"`
}

type AssetInspectInput struct {
	AssetID domain.AssetID `json:"assetId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Asset to inspect"`
}

type AssetFramesInput struct {
	AssetID        domain.AssetID        `json:"assetId" format:"uuid" doc:"Asset to sample"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId" format:"uuid" doc:"Exact video SourceStream to sample"`
	Times          []domain.RationalTime `json:"times" minItems:"1" maxItems:"8" doc:"Strictly increasing exact source times"`
}

type TranscriptReadInput struct {
	AssetID    domain.AssetID     `json:"assetId" format:"uuid" doc:"Asset whose transcript should be read"`
	ArtifactID *domain.ArtifactID `json:"artifactId,omitempty" format:"uuid" doc:"Exact artifact; omit for the Creator-selected default"`
	After      string             `json:"after,omitempty" maxLength:"10" doc:"Last segment ordinal returned by the previous page"`
	Limit      uint16             `json:"limit,omitempty" minimum:"1" maximum:"50" default:"20" doc:"Maximum transcript segments to return"`
}

type AssetListData = application.AssetPage
type AssetFramesData = application.MediaFrameSetRequestResult
type TranscriptReadData = application.TranscriptReadPage

type AssetInspectData struct {
	Asset          application.AssetView `json:"asset"`
	ActivityCursor domain.Cursor         `json:"activityCursor"`
}
