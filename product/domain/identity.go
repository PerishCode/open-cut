package domain

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidDurableID = errors.New("invalid canonical UUIDv7 durable ID")
	ErrInvalidRequestID = errors.New("invalid request identity")
	ErrInvalidLocalID   = errors.New("invalid proposal-local symbol")
)

var (
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	localIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type ID[Kind any] struct {
	value string
}

func ParseID[Kind any](value string) (ID[Kind], error) {
	if !isUUIDv7(value) {
		return ID[Kind]{}, ErrInvalidDurableID
	}
	return ID[Kind]{value: value}, nil
}

func (id ID[Kind]) String() string {
	return id.value
}

func (id ID[Kind]) IsZero() bool {
	return id.value == ""
}

func (id ID[Kind]) MarshalJSON() ([]byte, error) {
	if id.IsZero() || !isUUIDv7(id.value) {
		return nil, ErrInvalidDurableID
	}
	return json.Marshal(id.value)
}

func (id *ID[Kind]) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidDurableID
	}
	return id.UnmarshalText([]byte(value))
}

func (id ID[Kind]) MarshalText() ([]byte, error) {
	if id.IsZero() || !isUUIDv7(id.value) {
		return nil, ErrInvalidDurableID
	}
	return []byte(id.value), nil
}

func (id *ID[Kind]) UnmarshalText(text []byte) error {
	next, err := ParseID[Kind](string(text))
	if err != nil {
		return err
	}
	*id = next
	return nil
}

type projectIDKind struct{}
type projectVersionIDKind struct{}
type narrativeDocumentIDKind struct{}
type narrativeNodeIDKind struct{}
type sequenceIDKind struct{}
type trackIDKind struct{}
type proposalIDKind struct{}
type transactionIDKind struct{}
type activityEventIDKind struct{}
type creatorIDKind struct{}
type agentIDKind struct{}
type runIDKind struct{}
type turnIDKind struct{}
type captionIDKind struct{}
type alignmentIDKind struct{}
type clipIDKind struct{}
type linkGroupIDKind struct{}
type proposalApplicationIDKind struct{}
type sourceGrantIDKind struct{}
type assetIDKind struct{}
type sourceStreamIDKind struct{}
type artifactIDKind struct{}
type workJobIDKind struct{}
type jobAttemptIDKind struct{}
type resourceIDKind struct{}
type transcriptSegmentIDKind struct{}
type transcriptTokenIDKind struct{}
type transcriptCorrectionIDKind struct{}
type conversationMessageIDKind struct{}
type commandReceiptIDKind struct{}

type ProjectID = ID[projectIDKind]
type ProjectVersionID = ID[projectVersionIDKind]
type NarrativeDocumentID = ID[narrativeDocumentIDKind]
type NarrativeNodeID = ID[narrativeNodeIDKind]
type SequenceID = ID[sequenceIDKind]
type TrackID = ID[trackIDKind]
type ProposalID = ID[proposalIDKind]
type TransactionID = ID[transactionIDKind]
type ActivityEventID = ID[activityEventIDKind]
type CreatorID = ID[creatorIDKind]
type AgentID = ID[agentIDKind]
type RunID = ID[runIDKind]
type TurnID = ID[turnIDKind]
type CaptionID = ID[captionIDKind]
type AlignmentID = ID[alignmentIDKind]
type ClipID = ID[clipIDKind]
type LinkGroupID = ID[linkGroupIDKind]
type ProposalApplicationID = ID[proposalApplicationIDKind]
type SourceGrantID = ID[sourceGrantIDKind]
type AssetID = ID[assetIDKind]
type SourceStreamID = ID[sourceStreamIDKind]
type ArtifactID = ID[artifactIDKind]
type WorkJobID = ID[workJobIDKind]
type MediaJobID = WorkJobID
type JobAttemptID = ID[jobAttemptIDKind]
type ResourceID = ID[resourceIDKind]
type TranscriptSegmentID = ID[transcriptSegmentIDKind]
type TranscriptTokenID = ID[transcriptTokenIDKind]
type TranscriptCorrectionID = ID[transcriptCorrectionIDKind]
type ConversationMessageID = ID[conversationMessageIDKind]
type CommandReceiptID = ID[commandReceiptIDKind]

func ParseProjectID(value string) (ProjectID, error) {
	return ParseID[projectIDKind](value)
}

func ParseProjectVersionID(value string) (ProjectVersionID, error) {
	return ParseID[projectVersionIDKind](value)
}

func ParseNarrativeDocumentID(value string) (NarrativeDocumentID, error) {
	return ParseID[narrativeDocumentIDKind](value)
}

func ParseNarrativeNodeID(value string) (NarrativeNodeID, error) {
	return ParseID[narrativeNodeIDKind](value)
}

func ParseSequenceID(value string) (SequenceID, error) {
	return ParseID[sequenceIDKind](value)
}

func ParseTrackID(value string) (TrackID, error) {
	return ParseID[trackIDKind](value)
}

func ParseProposalID(value string) (ProposalID, error) {
	return ParseID[proposalIDKind](value)
}

func ParseTransactionID(value string) (TransactionID, error) {
	return ParseID[transactionIDKind](value)
}

func ParseActivityEventID(value string) (ActivityEventID, error) {
	return ParseID[activityEventIDKind](value)
}

func ParseCreatorID(value string) (CreatorID, error) {
	return ParseID[creatorIDKind](value)
}

func ParseAgentID(value string) (AgentID, error) {
	return ParseID[agentIDKind](value)
}

func ParseRunID(value string) (RunID, error) {
	return ParseID[runIDKind](value)
}

func ParseTurnID(value string) (TurnID, error) {
	return ParseID[turnIDKind](value)
}

func ParseCaptionID(value string) (CaptionID, error) {
	return ParseID[captionIDKind](value)
}

func ParseAlignmentID(value string) (AlignmentID, error) {
	return ParseID[alignmentIDKind](value)
}

func ParseClipID(value string) (ClipID, error) {
	return ParseID[clipIDKind](value)
}

func ParseLinkGroupID(value string) (LinkGroupID, error) {
	return ParseID[linkGroupIDKind](value)
}

func ParseProposalApplicationID(value string) (ProposalApplicationID, error) {
	return ParseID[proposalApplicationIDKind](value)
}

func ParseSourceGrantID(value string) (SourceGrantID, error) {
	return ParseID[sourceGrantIDKind](value)
}

func ParseAssetID(value string) (AssetID, error) {
	return ParseID[assetIDKind](value)
}

func ParseSourceStreamID(value string) (SourceStreamID, error) {
	return ParseID[sourceStreamIDKind](value)
}

func ParseArtifactID(value string) (ArtifactID, error) {
	return ParseID[artifactIDKind](value)
}

func ParseMediaJobID(value string) (MediaJobID, error) {
	return ParseWorkJobID(value)
}

func ParseWorkJobID(value string) (WorkJobID, error) {
	return ParseID[workJobIDKind](value)
}

func ParseJobAttemptID(value string) (JobAttemptID, error) {
	return ParseID[jobAttemptIDKind](value)
}

func ParseResourceID(value string) (ResourceID, error) {
	return ParseID[resourceIDKind](value)
}

func ParseTranscriptSegmentID(value string) (TranscriptSegmentID, error) {
	return ParseID[transcriptSegmentIDKind](value)
}

func ParseTranscriptTokenID(value string) (TranscriptTokenID, error) {
	return ParseID[transcriptTokenIDKind](value)
}

func ParseTranscriptCorrectionID(value string) (TranscriptCorrectionID, error) {
	return ParseID[transcriptCorrectionIDKind](value)
}

func ParseConversationMessageID(value string) (ConversationMessageID, error) {
	return ParseID[conversationMessageIDKind](value)
}

func ParseCommandReceiptID(value string) (CommandReceiptID, error) {
	return ParseID[commandReceiptIDKind](value)
}

type RequestID string

func ParseRequestID(value string) (RequestID, error) {
	if !requestIDPattern.MatchString(value) {
		return "", ErrInvalidRequestID
	}
	return RequestID(value), nil
}

func (id RequestID) String() string {
	return string(id)
}

type LocalID string

func ParseLocalID(value string) (LocalID, error) {
	if !localIDPattern.MatchString(value) {
		return "", ErrInvalidLocalID
	}
	return LocalID(value), nil
}

func (id LocalID) String() string {
	return string(id)
}

func GenerateUUIDv7(at time.Time) (string, error) {
	return GenerateUUIDv7From(at, rand.Reader)
}

func GenerateUUIDv7From(at time.Time, source io.Reader) (string, error) {
	milliseconds := at.UnixMilli()
	if milliseconds < 0 || uint64(milliseconds) >= 1<<48 {
		return "", fmt.Errorf("UUIDv7 time is outside the 48-bit Unix millisecond range")
	}
	var bytes [16]byte
	if _, err := io.ReadFull(source, bytes[:]); err != nil {
		return "", fmt.Errorf("read UUIDv7 randomness: %w", err)
	}
	timestamp := uint64(milliseconds)
	bytes[0] = byte(timestamp >> 40)
	bytes[1] = byte(timestamp >> 32)
	bytes[2] = byte(timestamp >> 24)
	bytes[3] = byte(timestamp >> 16)
	bytes[4] = byte(timestamp >> 8)
	bytes[5] = byte(timestamp)
	bytes[6] = 0x70 | bytes[6]&0x0f
	bytes[8] = 0x80 | bytes[8]&0x3f

	// google/uuid cannot mint a v7 for a caller-supplied time and entropy
	// source, so the packing above stays ours; the library owns canonical
	// formatting and the byte-level type.
	value := uuid.UUID(bytes).String()
	if !isUUIDv7(value) {
		return "", ErrInvalidDurableID
	}
	return value, nil
}

func isUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	if value[14] != '7' || !strings.ContainsRune("89ab", rune(value[19])) {
		return false
	}
	for index, current := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdef", current) {
			return false
		}
	}
	return true
}
