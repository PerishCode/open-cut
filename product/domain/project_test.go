package domain

import (
	"errors"
	"testing"
	"time"
)

var testInstant = time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

func TestProjectGenesisCreatesExplicitCreativeGraph(t *testing.T) {
	ids := testGenesisIDs(t)
	requestID, _ := ParseRequestID("gesture:create-project:001")
	genesis, err := NewProjectGenesis(ProjectGenesisInput{
		Name: "  Launch story  ", Format: DefaultSequenceFormat(),
		RequestID: requestID, Actor: testCreatorActor(), CreatedAt: testInstant,
	}, ids)
	if err != nil {
		t.Fatal(err)
	}
	project := genesis.Project
	if project.ID != ids.Project || project.Name != "  Launch story  " || project.Status != ProjectActive ||
		project.Revision.Value() != 1 || project.LifecycleRevision.Value() != 1 {
		t.Fatalf("project = %+v", project)
	}
	if len(project.NarrativeDocuments) != 1 || len(project.NarrativeDocuments[0].Nodes) != 1 ||
		project.NarrativeDocuments[0].Kind != NarrativeDocumentPaperEdit ||
		project.NarrativeDocuments[0].RootNodeID != ids.RootSection {
		t.Fatalf("narrative = %+v", project.NarrativeDocuments)
	}
	if len(project.Sequences) != 1 || project.Sequences[0].Name != "main" ||
		project.Sequences[0].Role != SequenceRoleMain || len(project.Sequences[0].Tracks) != 3 {
		t.Fatalf("sequences = %+v", project.Sequences)
	}
	tracks := project.Sequences[0].Tracks
	if tracks[0].Type != TrackVideo || tracks[0].Label != "V1" ||
		tracks[1].Type != TrackAudio || tracks[1].Label != "A1" ||
		tracks[2].Type != TrackCaption || tracks[2].Label != "C1" {
		t.Fatalf("tracks = %+v", tracks)
	}
	if genesis.Record.Actor.Kind != ActorCreator || genesis.Record.Actor.IDString() != testCreatorActor().IDString() ||
		genesis.Record.ProposalID != ids.Proposal ||
		genesis.Record.TransactionID != ids.Transaction || genesis.Record.CommittedProjectRevision.Value() != 1 {
		t.Fatalf("record = %+v", genesis.Record)
	}
}

func TestProjectGenesisRejectsInvalidInputWithoutRepair(t *testing.T) {
	ids := testGenesisIDs(t)
	requestID, _ := ParseRequestID("create-1")
	input := ProjectGenesisInput{
		Name: "   ", Format: DefaultSequenceFormat(), RequestID: requestID,
		Actor: testCreatorActor(), CreatedAt: testInstant,
	}
	if _, err := NewProjectGenesis(input, ids); !errors.Is(err, ErrInvalidProjectName) {
		t.Fatalf("name error = %v", err)
	}

	input.Name = "Valid"
	input.Format.CanvasWidth = 0
	if _, err := NewProjectGenesis(input, ids); !errors.Is(err, ErrInvalidSequenceFormat) {
		t.Fatalf("format error = %v", err)
	}

	input.Format = DefaultSequenceFormat()
	ids.AudioTrack = ids.VideoTrack
	if _, err := NewProjectGenesis(input, ids); !errors.Is(err, ErrInvalidGenesisIDs) {
		t.Fatalf("identity error = %v", err)
	}
}

func testCreatorActor() ActorRef {
	creator, _ := ParseCreatorID("018f0000-0000-7000-8000-00000000000b")
	return CreatorActor(creator)
}

func testGenesisIDs(t *testing.T) GenesisIDs {
	t.Helper()
	values := []string{
		"018f0000-0000-7000-8000-000000000001",
		"018f0000-0000-7000-8000-000000000002",
		"018f0000-0000-7000-8000-000000000003",
		"018f0000-0000-7000-8000-000000000004",
		"018f0000-0000-7000-8000-000000000005",
		"018f0000-0000-7000-8000-000000000006",
		"018f0000-0000-7000-8000-000000000007",
		"018f0000-0000-7000-8000-000000000008",
		"018f0000-0000-7000-8000-000000000009",
		"018f0000-0000-7000-8000-00000000000a",
		"018f0000-0000-7000-8000-00000000000c",
	}
	project, _ := ParseProjectID(values[0])
	version, _ := ParseProjectVersionID(values[1])
	document, _ := ParseNarrativeDocumentID(values[2])
	root, _ := ParseNarrativeNodeID(values[3])
	sequence, _ := ParseSequenceID(values[4])
	video, _ := ParseTrackID(values[5])
	audio, _ := ParseTrackID(values[6])
	caption, _ := ParseTrackID(values[7])
	proposal, _ := ParseProposalID(values[8])
	transaction, _ := ParseTransactionID(values[9])
	activityEvent, _ := ParseActivityEventID(values[10])
	return GenesisIDs{
		Project: project, ProjectVersion: version, NarrativeDocument: document, RootSection: root,
		MainSequence: sequence, VideoTrack: video, AudioTrack: audio, CaptionTrack: caption,
		Proposal: proposal, Transaction: transaction, ActivityEvent: activityEvent,
	}
}
