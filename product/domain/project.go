package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultCanvasWidth     = 1920
	DefaultCanvasHeight    = 1080
	DefaultAudioSampleRate = 48000
)

var (
	ErrInvalidProjectName    = errors.New("invalid project name")
	ErrInvalidSequenceFormat = errors.New("invalid sequence format")
	ErrInvalidGenesisIDs     = errors.New("invalid project genesis identities")
	ErrInvalidCreativeActor  = errors.New("invalid creative actor")
)

type ProjectStatus string

const (
	ProjectActive     ProjectStatus = "active"
	ProjectArchived   ProjectStatus = "archived"
	ProjectTombstoned ProjectStatus = "tombstoned"
	ProjectPurged     ProjectStatus = "purged"
)

type NarrativeDocumentKind string

const NarrativeDocumentPaperEdit NarrativeDocumentKind = "paper-edit"

type NarrativeNodeKind string

const (
	NarrativeNodeSection       NarrativeNodeKind = "section"
	NarrativeNodeAuthoredText  NarrativeNodeKind = "authored-text"
	NarrativeNodeSourceExcerpt NarrativeNodeKind = "source-excerpt"
	NarrativeNodeVisualIntent  NarrativeNodeKind = "visual-intent"
	NarrativeNodeNote          NarrativeNodeKind = "note"
)

type SequenceRole string

const SequenceRoleMain SequenceRole = "main"

type TrackType string

const (
	TrackVideo   TrackType = "video"
	TrackAudio   TrackType = "audio"
	TrackCaption TrackType = "caption"
)

type AudioLayout string

const AudioStereo AudioLayout = "stereo"

type ColorPolicy string

const ColorSDRRec709 ColorPolicy = "sdr-rec709"

type CreativeActor string

const (
	ActorCreator CreativeActor = "creator"
	ActorAgent   CreativeActor = "agent"
)

type ActorRef struct {
	Kind      CreativeActor `json:"kind" enum:"creator,agent"`
	CreatorID *CreatorID    `json:"creatorId,omitempty"`
	AgentID   *AgentID      `json:"agentId,omitempty"`
}

func CreatorActor(id CreatorID) ActorRef {
	return ActorRef{Kind: ActorCreator, CreatorID: &id}
}

func AgentActor(id AgentID) ActorRef {
	return ActorRef{Kind: ActorAgent, AgentID: &id}
}

func (actor ActorRef) Validate() error {
	switch actor.Kind {
	case ActorCreator:
		if actor.CreatorID == nil || actor.CreatorID.IsZero() || actor.AgentID != nil {
			return ErrInvalidCreativeActor
		}
	case ActorAgent:
		if actor.AgentID == nil || actor.AgentID.IsZero() || actor.CreatorID != nil {
			return ErrInvalidCreativeActor
		}
	default:
		return ErrInvalidCreativeActor
	}
	return nil
}

func (actor ActorRef) IDString() string {
	if actor.Kind == ActorCreator && actor.CreatorID != nil {
		return actor.CreatorID.String()
	}
	if actor.Kind == ActorAgent && actor.AgentID != nil {
		return actor.AgentID.String()
	}
	return ""
}

type SequenceFormat struct {
	CanvasWidth     uint32       `json:"canvasWidth" minimum:"16" maximum:"16384"`
	CanvasHeight    uint32       `json:"canvasHeight" minimum:"16" maximum:"16384"`
	PixelAspect     RationalTime `json:"pixelAspect"`
	FrameRate       RationalTime `json:"frameRate"`
	AudioSampleRate uint32       `json:"audioSampleRate" minimum:"8000" maximum:"384000"`
	AudioLayout     AudioLayout  `json:"audioLayout" enum:"stereo"`
	ColorPolicy     ColorPolicy  `json:"colorPolicy" enum:"sdr-rec709"`
}

func DefaultSequenceFormat() SequenceFormat {
	one, _ := NewRationalTime(1, 1)
	frameRate, _ := NewRationalTime(30, 1)
	return SequenceFormat{
		CanvasWidth: DefaultCanvasWidth, CanvasHeight: DefaultCanvasHeight,
		PixelAspect: one, FrameRate: frameRate,
		AudioSampleRate: DefaultAudioSampleRate, AudioLayout: AudioStereo,
		ColorPolicy: ColorSDRRec709,
	}
}

func (format SequenceFormat) Validate() error {
	if format.CanvasWidth < 16 || format.CanvasWidth > 16384 ||
		format.CanvasHeight < 16 || format.CanvasHeight > 16384 {
		return ErrInvalidSequenceFormat
	}
	if err := format.PixelAspect.Validate(); err != nil || !format.PixelAspect.IsPositive() {
		return ErrInvalidSequenceFormat
	}
	if err := format.FrameRate.Validate(); err != nil || !format.FrameRate.IsPositive() {
		return ErrInvalidSequenceFormat
	}
	maximumRate, _ := NewRationalTime(240, 1)
	if comparison, err := format.FrameRate.Compare(maximumRate); err != nil || comparison > 0 {
		return ErrInvalidSequenceFormat
	}
	if format.AudioSampleRate < 8000 || format.AudioSampleRate > 384000 ||
		format.AudioLayout != AudioStereo || format.ColorPolicy != ColorSDRRec709 {
		return ErrInvalidSequenceFormat
	}
	return nil
}

type Project struct {
	ID                 ProjectID           `json:"id"`
	Revision           Revision            `json:"revision"`
	LifecycleRevision  Revision            `json:"lifecycleRevision"`
	Name               string              `json:"name"`
	Status             ProjectStatus       `json:"status"`
	NarrativeDocuments []NarrativeDocument `json:"narrativeDocuments"`
	Sequences          []Sequence          `json:"sequences"`
}

type NarrativeDocument struct {
	ID         NarrativeDocumentID   `json:"id"`
	Revision   Revision              `json:"revision"`
	Kind       NarrativeDocumentKind `json:"kind"`
	RootNodeID NarrativeNodeID       `json:"rootNodeId"`
	Nodes      []NarrativeNode       `json:"nodes"`
}

type NarrativeNode struct {
	ID         NarrativeNodeID   `json:"id"`
	Revision   Revision          `json:"revision"`
	Kind       NarrativeNodeKind `json:"kind"`
	ParentID   *NarrativeNodeID  `json:"parentId,omitempty"`
	Title      string            `json:"title,omitempty"`
	Language   CaptionLanguage   `json:"language" maxLength:"64"`
	Text       string            `json:"text,omitempty"`
	Tombstoned bool              `json:"tombstoned"`
	OrderKey   string            `json:"-"`
}

type Sequence struct {
	ID       SequenceID     `json:"id"`
	Revision Revision       `json:"revision"`
	Name     string         `json:"name"`
	Role     SequenceRole   `json:"role"`
	Format   SequenceFormat `json:"format"`
	Tracks   []Track        `json:"tracks"`
}

type Track struct {
	ID       TrackID   `json:"id"`
	Revision Revision  `json:"revision"`
	Type     TrackType `json:"type"`
	Label    string    `json:"label"`
	OrderKey string    `json:"-"`
}

type GenesisIDs struct {
	Project           ProjectID
	NarrativeDocument NarrativeDocumentID
	RootSection       NarrativeNodeID
	MainSequence      SequenceID
	VideoTrack        TrackID
	AudioTrack        TrackID
	CaptionTrack      TrackID
	Proposal          ProposalID
	Transaction       TransactionID
	ActivityEvent     ActivityEventID
}

type ProjectGenesisInput struct {
	Name      string
	Format    SequenceFormat
	RequestID RequestID
	Actor     ActorRef
	CreatedAt time.Time
}

type GenesisRecord struct {
	ProposalID               ProposalID      `json:"proposalId"`
	TransactionID            TransactionID   `json:"transactionId"`
	RequestID                RequestID       `json:"requestId"`
	Actor                    ActorRef        `json:"actor"`
	CommittedProjectRevision Revision        `json:"committedProjectRevision"`
	ProposalDigest           Digest          `json:"proposalDigest"`
	ActivityEventID          ActivityEventID `json:"activityEventId"`
	CreatedAt                time.Time       `json:"createdAt"`
}

type ProjectGenesis struct {
	Project Project       `json:"project"`
	Record  GenesisRecord `json:"record"`
}

func NewProjectGenesis(input ProjectGenesisInput, ids GenesisIDs) (ProjectGenesis, error) {
	if err := validateProjectName(input.Name); err != nil {
		return ProjectGenesis{}, err
	}
	if err := input.Format.Validate(); err != nil {
		return ProjectGenesis{}, err
	}
	if _, err := ParseRequestID(input.RequestID.String()); err != nil {
		return ProjectGenesis{}, err
	}
	if err := input.Actor.Validate(); err != nil || input.Actor.Kind != ActorCreator {
		return ProjectGenesis{}, ErrInvalidCreativeActor
	}
	if input.CreatedAt.IsZero() {
		return ProjectGenesis{}, fmt.Errorf("genesis creation instant is required")
	}
	if err := validateGenesisIDs(ids); err != nil {
		return ProjectGenesis{}, err
	}
	revision, _ := NewRevision(1)
	rootLanguage, _ := ParseCaptionLanguage("und")
	root := NarrativeNode{
		ID: ids.RootSection, Revision: revision, Kind: NarrativeNodeSection,
		Title: "", Language: rootLanguage,
	}
	document := NarrativeDocument{
		ID: ids.NarrativeDocument, Revision: revision, Kind: NarrativeDocumentPaperEdit,
		RootNodeID: ids.RootSection, Nodes: []NarrativeNode{root},
	}
	sequence := Sequence{
		ID: ids.MainSequence, Revision: revision, Name: "main", Role: SequenceRoleMain, Format: input.Format,
		Tracks: []Track{
			{ID: ids.VideoTrack, Revision: revision, Type: TrackVideo, Label: "V1", OrderKey: "a0"},
			{ID: ids.AudioTrack, Revision: revision, Type: TrackAudio, Label: "A1", OrderKey: "a1"},
			{ID: ids.CaptionTrack, Revision: revision, Type: TrackCaption, Label: "C1", OrderKey: "a2"},
		},
	}
	genesis := ProjectGenesis{
		Project: Project{
			ID: ids.Project, Revision: revision, LifecycleRevision: revision,
			Name: input.Name, Status: ProjectActive,
			NarrativeDocuments: []NarrativeDocument{document}, Sequences: []Sequence{sequence},
		},
		Record: GenesisRecord{
			ProposalID: ids.Proposal, TransactionID: ids.Transaction,
			ActivityEventID: ids.ActivityEvent,
			RequestID:       input.RequestID, Actor: input.Actor,
			CommittedProjectRevision: revision, CreatedAt: input.CreatedAt.UTC(),
		},
	}
	proposalDigest, err := ProjectGenesisProposalDigest(genesis)
	if err != nil {
		return ProjectGenesis{}, err
	}
	genesis.Record.ProposalDigest = proposalDigest
	return genesis, nil
}

func validateProjectName(name string) error {
	if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 200 {
		return ErrInvalidProjectName
	}
	return nil
}

func validateGenesisIDs(ids GenesisIDs) error {
	values := []string{
		ids.Project.String(), ids.NarrativeDocument.String(), ids.RootSection.String(),
		ids.MainSequence.String(), ids.VideoTrack.String(), ids.AudioTrack.String(),
		ids.CaptionTrack.String(), ids.Proposal.String(), ids.Transaction.String(),
		ids.ActivityEvent.String(),
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !isUUIDv7(value) {
			return ErrInvalidGenesisIDs
		}
		if _, exists := seen[value]; exists {
			return ErrInvalidGenesisIDs
		}
		seen[value] = struct{}{}
	}
	return nil
}
