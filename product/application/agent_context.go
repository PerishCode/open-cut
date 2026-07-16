package application

import (
	"encoding/json"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MaximumAgentContextAttachments = 64
	MaximumAgentContextBytes       = 16 * 1024
)

type AgentContextAttachmentKind string

const (
	AgentContextAsset             AgentContextAttachmentKind = "asset"
	AgentContextTranscriptSegment AgentContextAttachmentKind = "transcript-segment"
	AgentContextNarrativeNode     AgentContextAttachmentKind = "narrative-node"
	AgentContextClip              AgentContextAttachmentKind = "clip"
	AgentContextCaption           AgentContextAttachmentKind = "caption"
	AgentContextTrack             AgentContextAttachmentKind = "track"
	AgentContextSequencePoint     AgentContextAttachmentKind = "sequence-point"
	AgentContextSequenceRange     AgentContextAttachmentKind = "sequence-range"
)

type AgentContextEntityRef struct {
	ID       string          `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Revision domain.Revision `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type AgentContextTranscriptRef struct {
	ArtifactID domain.ArtifactID          `json:"artifactId" format:"uuid"`
	SegmentID  domain.TranscriptSegmentID `json:"segmentId" format:"uuid"`
}

type AgentContextSequencePointRef struct {
	SequenceID domain.SequenceID   `json:"sequenceId" format:"uuid"`
	Revision   domain.Revision     `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Time       domain.RationalTime `json:"time"`
}

type AgentContextSequenceRangeRef struct {
	SequenceID domain.SequenceID `json:"sequenceId" format:"uuid"`
	Revision   domain.Revision   `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Range      domain.TimeRange  `json:"range"`
}

type AgentContextAttachment struct {
	Kind       AgentContextAttachmentKind    `json:"kind" enum:"asset,transcript-segment,narrative-node,clip,caption,track,sequence-point,sequence-range"`
	Entity     *AgentContextEntityRef        `json:"entity,omitempty"`
	Transcript *AgentContextTranscriptRef    `json:"transcript,omitempty"`
	Point      *AgentContextSequencePointRef `json:"point,omitempty"`
	Range      *AgentContextSequenceRangeRef `json:"range,omitempty"`
}

func ValidateAgentContextAttachments(values []AgentContextAttachment) error {
	if len(values) > MaximumAgentContextAttachments {
		return ErrAgentBridgeInvalid
	}
	total := 0
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validAgentContextAttachment(value) {
			return ErrAgentBridgeInvalid
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return ErrAgentBridgeInvalid
		}
		total += len(encoded)
		key := string(encoded)
		if _, exists := seen[key]; exists {
			return ErrAgentBridgeInvalid
		}
		seen[key] = struct{}{}
	}
	if total > MaximumAgentContextBytes {
		return ErrAgentBridgeInvalid
	}
	return nil
}

func validAgentContextAttachment(value AgentContextAttachment) bool {
	count := 0
	for _, present := range []bool{value.Entity != nil, value.Transcript != nil, value.Point != nil, value.Range != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return false
	}
	switch value.Kind {
	case AgentContextAsset, AgentContextNarrativeNode, AgentContextClip, AgentContextCaption, AgentContextTrack:
		return value.Entity != nil && validAgentContextEntityID(value.Kind, value.Entity.ID) && value.Entity.Revision.Value() > 0
	case AgentContextTranscriptSegment:
		return value.Transcript != nil && !value.Transcript.ArtifactID.IsZero() && !value.Transcript.SegmentID.IsZero()
	case AgentContextSequencePoint:
		return value.Point != nil && !value.Point.SequenceID.IsZero() && value.Point.Revision.Value() > 0 &&
			value.Point.Time.Validate() == nil
	case AgentContextSequenceRange:
		return value.Range != nil && !value.Range.SequenceID.IsZero() && value.Range.Revision.Value() > 0 &&
			value.Range.Range.Start.Validate() == nil && value.Range.Range.Duration.Validate() == nil &&
			value.Range.Range.Duration.IsPositive()
	default:
		return false
	}
}

func validAgentContextEntityID(kind AgentContextAttachmentKind, value string) bool {
	var err error
	switch kind {
	case AgentContextAsset:
		_, err = domain.ParseAssetID(value)
	case AgentContextNarrativeNode:
		_, err = domain.ParseNarrativeNodeID(value)
	case AgentContextClip:
		_, err = domain.ParseClipID(value)
	case AgentContextCaption:
		_, err = domain.ParseCaptionID(value)
	case AgentContextTrack:
		_, err = domain.ParseTrackID(value)
	default:
		return false
	}
	return err == nil
}
