package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestValidateEditProposeInputReportsActionableReasons(t *testing.T) {
	nodeID, err := domain.ParseNarrativeNodeID("018f0000-0000-7000-8000-0000000000a1")
	if err != nil {
		t.Fatal(err)
	}
	node := nodeID.String()
	requestID, err := domain.ParseRequestID("edit-reasons-test")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := domain.NewRevision(3)
	if err != nil {
		t.Fatal(err)
	}
	one, err := domain.NewRevision(1)
	if err != nil {
		t.Fatal(err)
	}
	goodOp := EditOperationInput{
		Type: domain.EditInsertAuthoredText, CreateAs: localID(t, "a1"), ParentID: &nodeID,
		Text: stringPointer("hello"), AuthoredTextPurpose: authoredPurpose(t, "spoken"),
		Language: captionLanguage(t, "en"),
	}
	base := EditProposeInput{
		RequestID: requestID, Intent: "reason coverage", BaseProjectRevision: revision,
		Operations: []EditOperationInput{goodOp},
	}
	duplicate := base
	duplicate.Preconditions = []domain.EntityPrecondition{
		{Kind: domain.EntityNarrativeNode, ID: node, Revision: one},
		{Kind: domain.EntityNarrativeNode, ID: node, Revision: one},
	}
	empty := base
	empty.Operations = nil

	for _, testCase := range []struct {
		name  string
		input EditProposeInput
		want  string
	}{
		{"duplicate precondition", duplicate, "duplicate precondition"},
		{"empty operations", empty, "1 to 512 operations"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateEditProposeInput(testCase.input)
			if !errors.Is(err, ErrEditInvalid) {
				t.Fatalf("expected ErrEditInvalid, got %v", err)
			}
			var invalid EditInvalidError
			if !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, testCase.want) {
				t.Fatalf("reason %q does not mention %q", invalid.Reason, testCase.want)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func localID(t *testing.T, value string) *domain.LocalID {
	t.Helper()
	parsed, err := domain.ParseLocalID(value)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed
}

func authoredPurpose(t *testing.T, value string) *domain.AuthoredTextPurpose {
	t.Helper()
	parsed := domain.AuthoredTextPurpose(value)
	return &parsed
}

func captionLanguage(t *testing.T, value string) *domain.CaptionLanguage {
	t.Helper()
	parsed, err := domain.ParseCaptionLanguage(value)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed
}
