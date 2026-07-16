package application

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SequenceRenderIntentSchema             = "open-cut/sequence-render-intent/v1"
	SequencePreviewRenderIntentSchema      = SequenceRenderIntentSchema
	MaximumSequencePreviewRenderIntentSize = 64 << 20
)

type SequencePreviewIntentTrack struct {
	ID       domain.TrackID   `json:"id"`
	Revision domain.Revision  `json:"revision"`
	Type     domain.TrackType `json:"type"`
	OrderKey string           `json:"orderKey"`
}

type SequencePreviewIntentClip struct {
	ID             domain.ClipID         `json:"id"`
	Revision       domain.Revision       `json:"revision"`
	TrackID        domain.TrackID        `json:"trackId"`
	AssetID        domain.AssetID        `json:"assetId"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
	SourceRange    domain.TimeRange      `json:"sourceRange"`
	TimelineRange  domain.TimeRange      `json:"timelineRange"`
}

type SequencePreviewIntentCaption struct {
	ID       domain.CaptionID       `json:"id"`
	Revision domain.Revision        `json:"revision"`
	TrackID  domain.TrackID         `json:"trackId"`
	Range    domain.TimeRange       `json:"range"`
	Language domain.CaptionLanguage `json:"language"`
	Text     string                 `json:"text"`
}

type SequencePreviewIntentAsset struct {
	ID                  domain.AssetID  `json:"id"`
	Revision            domain.Revision `json:"revision"`
	AcceptedFingerprint domain.Digest   `json:"acceptedFingerprint"`
}

type SequencePreviewRenderIntent struct {
	ProjectID               domain.ProjectID               `json:"projectId"`
	ObservedProjectRevision domain.Revision                `json:"observedProjectRevision"`
	SequenceID              domain.SequenceID              `json:"sequenceId"`
	SequenceRevision        domain.Revision                `json:"sequenceRevision"`
	SequenceFormat          domain.SequenceFormat          `json:"sequenceFormat"`
	Tracks                  []SequencePreviewIntentTrack   `json:"tracks"`
	Clips                   []SequencePreviewIntentClip    `json:"clips"`
	Captions                []SequencePreviewIntentCaption `json:"captions"`
	Assets                  []SequencePreviewIntentAsset   `json:"assets"`
}

type SequenceRenderIntent = SequencePreviewRenderIntent

func NewSequencePreviewRenderIntent(
	snapshot SequencePreviewPreparationSnapshot,
	inputs []SequencePreviewInputRequirement,
) (SequencePreviewRenderIntent, []byte, domain.Digest, error) {
	intent := SequencePreviewRenderIntent{
		ProjectID: snapshot.ProjectID, ObservedProjectRevision: snapshot.ObservedProjectRevision,
		SequenceID: snapshot.Sequence.ID, SequenceRevision: snapshot.Sequence.Revision,
		SequenceFormat: snapshot.Sequence.Format,
		Tracks:         make([]SequencePreviewIntentTrack, 0, len(snapshot.Sequence.Tracks)),
		Clips:          []SequencePreviewIntentClip{}, Captions: []SequencePreviewIntentCaption{},
		Assets: []SequencePreviewIntentAsset{},
	}
	for _, track := range snapshot.Sequence.Tracks {
		intent.Tracks = append(intent.Tracks, SequencePreviewIntentTrack{
			ID: track.ID, Revision: track.Revision, Type: track.Type, OrderKey: track.OrderKey,
		})
	}
	assets := make(map[string]struct{})
	for _, clip := range snapshot.Clips {
		if !clip.Enabled || clip.Tombstoned {
			continue
		}
		intent.Clips = append(intent.Clips, SequencePreviewIntentClip{
			ID: clip.ID, Revision: clip.Revision, TrackID: clip.TrackID,
			AssetID: clip.AssetID, SourceStreamID: clip.SourceStreamID,
			SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange,
		})
		assets[clip.AssetID.String()] = struct{}{}
	}
	for _, caption := range snapshot.Captions {
		if caption.Tombstoned {
			continue
		}
		intent.Captions = append(intent.Captions, SequencePreviewIntentCaption{
			ID: caption.ID, Revision: caption.Revision, TrackID: caption.TrackID,
			Range: caption.Range, Language: caption.Language, Text: caption.Text,
		})
	}
	for assetID := range assets {
		asset, exists := snapshot.Assets[assetID]
		if !exists {
			return SequencePreviewRenderIntent{}, nil, "", ErrSequencePreviewInvalid
		}
		intent.Assets = append(intent.Assets, SequencePreviewIntentAsset{
			ID: asset.ID, Revision: asset.Revision, AcceptedFingerprint: asset.AcceptedFingerprint,
		})
	}
	return CanonicalSequencePreviewRenderIntent(intent, inputs)
}

func CanonicalSequencePreviewRenderIntent(
	intent SequencePreviewRenderIntent,
	inputs []SequencePreviewInputRequirement,
) (SequencePreviewRenderIntent, []byte, domain.Digest, error) {
	normalized := normalizeSequencePreviewRenderIntent(intent)
	if err := normalized.Validate(inputs); err != nil {
		return SequencePreviewRenderIntent{}, nil, "", err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-render-intent", SequenceRenderIntentSchema, normalized,
	)
	if err != nil || len(canonical) == 0 || len(canonical) > MaximumSequencePreviewRenderIntentSize {
		return SequencePreviewRenderIntent{}, nil, "", ErrSequencePreviewInvalid
	}
	return normalized, canonical, digest, nil
}

func DecodeSequencePreviewRenderIntent(
	data []byte,
	inputs []SequencePreviewInputRequirement,
) (SequencePreviewRenderIntent, domain.Digest, error) {
	if len(data) == 0 || len(data) > MaximumSequencePreviewRenderIntentSize {
		return SequencePreviewRenderIntent{}, "", ErrSequencePreviewInvalid
	}
	var envelope struct {
		Domain  string                      `json:"domain"`
		Payload SequencePreviewRenderIntent `json:"payload"`
		Schema  string                      `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-render-intent" ||
		envelope.Schema != SequenceRenderIntentSchema {
		return SequencePreviewRenderIntent{}, "", ErrSequencePreviewInvalid
	}
	normalized, canonical, digest, err := CanonicalSequencePreviewRenderIntent(envelope.Payload, inputs)
	if err != nil || !bytes.Equal(canonical, data) {
		return SequencePreviewRenderIntent{}, "", ErrSequencePreviewInvalid
	}
	return normalized, digest, nil
}

func NewSequenceRenderIntent(
	snapshot SequencePreviewPreparationSnapshot,
	inputs []SequencePreviewInputRequirement,
) (SequenceRenderIntent, []byte, domain.Digest, error) {
	return NewSequencePreviewRenderIntent(snapshot, inputs)
}

func CanonicalSequenceRenderIntent(
	intent SequenceRenderIntent,
	inputs []SequencePreviewInputRequirement,
) (SequenceRenderIntent, []byte, domain.Digest, error) {
	return CanonicalSequencePreviewRenderIntent(intent, inputs)
}

func DecodeSequenceRenderIntent(
	data []byte,
	inputs []SequencePreviewInputRequirement,
) (SequenceRenderIntent, domain.Digest, error) {
	return DecodeSequencePreviewRenderIntent(data, inputs)
}

func (intent SequencePreviewRenderIntent) Validate(inputs []SequencePreviewInputRequirement) error {
	if intent.ProjectID.IsZero() || intent.ObservedProjectRevision.Value() == 0 ||
		intent.SequenceID.IsZero() || intent.SequenceRevision.Value() == 0 ||
		intent.SequenceFormat.Validate() != nil || len(intent.Tracks) == 0 ||
		len(intent.Tracks) > MaximumRenderPlanItems || len(intent.Clips) > MaximumRenderPlanItems ||
		len(intent.Captions) > MaximumRenderPlanItems || len(intent.Assets) > MaximumRenderPlanItems ||
		len(intent.Clips) != len(inputs) || (len(intent.Clips) == 0 && len(intent.Captions) == 0) {
		return ErrSequencePreviewInvalid
	}
	sequence := intent.sequence()
	tracks, err := normalizeRenderTracks(sequence.Tracks)
	if err != nil {
		return ErrSequencePreviewInvalid
	}
	inputByClip := make(map[string]SequencePreviewInputRequirement, len(inputs))
	for _, input := range inputs {
		if input.ClipID.IsZero() || input.SourceStreamID.IsZero() || input.ProducerJobID.IsZero() {
			return ErrSequencePreviewInvalid
		}
		if _, duplicate := inputByClip[input.ClipID.String()]; duplicate {
			return ErrSequencePreviewInvalid
		}
		inputByClip[input.ClipID.String()] = input
	}
	assets := make(map[string]SequencePreviewIntentAsset, len(intent.Assets))
	for _, asset := range intent.Assets {
		if asset.ID.IsZero() || asset.Revision.Value() == 0 || asset.AcceptedFingerprint == "" {
			return ErrSequencePreviewInvalid
		}
		if _, err := domain.ParseDigest(asset.AcceptedFingerprint.String()); err != nil {
			return ErrSequencePreviewInvalid
		}
		if _, duplicate := assets[asset.ID.String()]; duplicate {
			return ErrSequencePreviewInvalid
		}
		assets[asset.ID.String()] = asset
	}
	usedAssets := make(map[string]struct{})
	for _, clip := range intent.Clips {
		state := clip.state(intent.SequenceID)
		if validateRenderClipHead(state, intent.SequenceID, tracks) != nil {
			return ErrSequencePreviewInvalid
		}
		input, exists := inputByClip[clip.ID.String()]
		if !exists || input.SourceStreamID != clip.SourceStreamID {
			return ErrSequencePreviewInvalid
		}
		if _, exists := assets[clip.AssetID.String()]; !exists {
			return ErrSequencePreviewInvalid
		}
		usedAssets[clip.AssetID.String()] = struct{}{}
	}
	if len(usedAssets) != len(assets) {
		return ErrSequencePreviewInvalid
	}
	for _, caption := range intent.Captions {
		track, exists := tracks[caption.TrackID.String()]
		if caption.ID.IsZero() || caption.Revision.Value() == 0 || !exists ||
			track.state.Type != domain.TrackCaption || validatePositiveRange(caption.Range, true) != nil ||
			caption.Language.Validate() != nil || caption.Text == "" || !utf8.ValidString(caption.Text) ||
			len([]byte(caption.Text)) > domain.MaximumAuthoredTextBytes {
			return ErrSequencePreviewInvalid
		}
	}
	return nil
}

func (intent SequencePreviewRenderIntent) CompileInput(
	bindings []RenderClipInputBinding,
	font *domain.RenderFontResource,
) CompileRenderPlanInput {
	assets := make(map[string]RenderAssetSnapshot, len(intent.Assets))
	for _, asset := range intent.Assets {
		assets[asset.ID.String()] = RenderAssetSnapshot{
			ID: asset.ID, Revision: asset.Revision, AcceptedFingerprint: asset.AcceptedFingerprint,
			Availability: domain.AssetOnline,
		}
	}
	clips := make([]domain.ClipState, 0, len(intent.Clips))
	for _, clip := range intent.Clips {
		clips = append(clips, clip.state(intent.SequenceID))
	}
	captions := make([]domain.CaptionState, 0, len(intent.Captions))
	for _, caption := range intent.Captions {
		captions = append(captions, domain.CaptionState{
			ID: caption.ID, Revision: caption.Revision, SequenceID: intent.SequenceID,
			TrackID: caption.TrackID, Range: caption.Range, Language: caption.Language, Text: caption.Text,
		})
	}
	return CompileRenderPlanInput{
		ProjectID: intent.ProjectID, ObservedProjectRevision: intent.ObservedProjectRevision,
		Sequence: intent.sequence(), Clips: clips, Captions: captions,
		Assets: assets, Bindings: bindings, FontResource: font,
	}
}

func normalizeSequencePreviewRenderIntent(intent SequencePreviewRenderIntent) SequencePreviewRenderIntent {
	intent.Tracks = append([]SequencePreviewIntentTrack(nil), intent.Tracks...)
	intent.Clips = append([]SequencePreviewIntentClip(nil), intent.Clips...)
	intent.Captions = append([]SequencePreviewIntentCaption(nil), intent.Captions...)
	intent.Assets = append([]SequencePreviewIntentAsset(nil), intent.Assets...)
	sort.Slice(intent.Tracks, func(left, right int) bool {
		if intent.Tracks[left].OrderKey != intent.Tracks[right].OrderKey {
			return intent.Tracks[left].OrderKey < intent.Tracks[right].OrderKey
		}
		return intent.Tracks[left].ID.String() < intent.Tracks[right].ID.String()
	})
	sort.Slice(intent.Clips, func(left, right int) bool {
		return intent.Clips[left].ID.String() < intent.Clips[right].ID.String()
	})
	sort.Slice(intent.Captions, func(left, right int) bool {
		return intent.Captions[left].ID.String() < intent.Captions[right].ID.String()
	})
	sort.Slice(intent.Assets, func(left, right int) bool {
		return intent.Assets[left].ID.String() < intent.Assets[right].ID.String()
	})
	return intent
}

func (intent SequencePreviewRenderIntent) sequence() domain.Sequence {
	tracks := make([]domain.Track, 0, len(intent.Tracks))
	for _, track := range intent.Tracks {
		tracks = append(tracks, domain.Track{
			ID: track.ID, Revision: track.Revision, Type: track.Type, OrderKey: track.OrderKey,
		})
	}
	return domain.Sequence{
		ID: intent.SequenceID, Revision: intent.SequenceRevision,
		Role: domain.SequenceRoleMain, Format: intent.SequenceFormat, Tracks: tracks,
	}
}

func (clip SequencePreviewIntentClip) state(sequenceID domain.SequenceID) domain.ClipState {
	return domain.ClipState{
		ID: clip.ID, Revision: clip.Revision, SequenceID: sequenceID,
		TrackID: clip.TrackID, AssetID: clip.AssetID, SourceStreamID: clip.SourceStreamID,
		SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange, Enabled: true,
	}
}
