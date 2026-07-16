package controller

import (
	"context"
	"net/http"
	"strconv"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

type narrativeShowHTTPInput struct {
	ProjectID  domain.ProjectID           `path:"projectId"`
	DocumentID domain.NarrativeDocumentID `path:"documentId"`
	ParentID   domain.NarrativeNodeID     `query:"parentId" required:"true"`
	After      string                     `query:"after" maxLength:"512"`
	Limit      uint16                     `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

type sequenceShowHTTPInput struct {
	ProjectID     domain.ProjectID  `path:"projectId"`
	SequenceID    domain.SequenceID `path:"sequenceId"`
	TrackID       string            `query:"trackId" format:"uuid"`
	StartValue    string            `query:"startValue" format:"int64-decimal" pattern:"^(0|-[1-9][0-9]*|[1-9][0-9]*)$" required:"true"`
	StartScale    int32             `query:"startScale" minimum:"1" required:"true"`
	DurationValue string            `query:"durationValue" format:"int64-decimal" pattern:"^[1-9][0-9]*$" required:"true"`
	DurationScale int32             `query:"durationScale" minimum:"1" required:"true"`
	After         string            `query:"after" maxLength:"512"`
	Limit         uint16            `query:"limit" minimum:"1" maximum:"512" default:"100"`
}

type entityShowHTTPInput struct {
	ProjectID domain.ProjectID      `path:"projectId"`
	Kind      domain.EditEntityKind `path:"kind" enum:"narrative-node,transcript-correction,caption,alignment,clip,link-group"`
	ID        string                `path:"id" format:"uuid"`
}

type editShowHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	ProposalID domain.ProposalID `path:"proposalId"`
}

type editHistoryHTTPInput struct {
	ProjectID domain.ProjectID `path:"projectId"`
	After     string           `query:"after" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Limit     uint16           `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type captionDeriveHTTPInput struct {
	ProjectID       domain.ProjectID       `path:"projectId"`
	SequenceID      domain.SequenceID      `path:"sequenceId"`
	SourceExcerptID domain.NarrativeNodeID `query:"sourceExcerptId" required:"true"`
	ClipID          domain.ClipID          `query:"clipId" required:"true"`
	TrackID         domain.TrackID         `query:"trackId" required:"true"`
	LocalPrefix     string                 `query:"localPrefix" pattern:"^[a-z][a-z0-9_-]{0,39}$" default:"derived"`
}

type roughCutDeriveHTTPInput struct {
	ProjectID  domain.ProjectID  `path:"projectId"`
	SequenceID domain.SequenceID `path:"sequenceId"`
	Body       command.RoughCutDeriveInput
}

type narrativeShowHTTPOutput struct{ Body command.NarrativeShowData }
type sequenceShowHTTPOutput struct{ Body command.SequenceShowData }
type entityShowHTTPOutput struct{ Body command.EntityShowData }
type editShowHTTPOutput struct{ Body command.EditShowData }
type editHistoryHTTPOutput struct{ Body command.EditHistoryData }
type captionDeriveHTTPOutput struct{ Body command.CaptionDeriveData }
type roughCutDeriveHTTPOutput struct{ Body command.RoughCutDeriveData }

func RegisterEditReads(
	api huma.API,
	reads *application.EditReads,
	runs *application.AgentRuns,
	authorizer service.Authorizer,
) {
	huma.Register(api, huma.Operation{
		OperationID: "derive-rough-cut-operation", Method: http.MethodPost,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/edit/rough-cut-derivation",
		Summary: "Preview one deterministic PaperEdit-to-Sequence rough-cut operation", Tags: []string{"editing"},
		Middlewares: requireCommandBodyAuthority(api, runs, authorizer, "edit", "derive-rough-cut"),
		Extensions:  commandExtensions("edit", "derive-rough-cut"),
	}, func(ctx context.Context, input *roughCutDeriveHTTPInput) (*roughCutDeriveHTTPOutput, error) {
		preview, err := reads.RoughCutDerivation(ctx, input.ProjectID, input.SequenceID, input.Body)
		if err != nil {
			return nil, editError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, preview, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("project", input.ProjectID.String(), preview.BaseProjectRevision),
				commandReceiptRef("sequence", input.SequenceID.String(), 0),
			}, preview.BaseProjectRevision, preview.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &roughCutDeriveHTTPOutput{Body: preview}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "derive-caption-operation", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/edit/caption-derivation",
		Summary: "Preview one deterministic SourceExcerpt-to-Clip caption operation", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "edit", "derive-captions"),
		Extensions:  commandExtensions("edit", "derive-captions"),
	}, func(ctx context.Context, input *captionDeriveHTTPInput) (*captionDeriveHTTPOutput, error) {
		preview, err := reads.CaptionDerivation(ctx, input.ProjectID, input.SequenceID,
			application.CaptionDerivationPreviewInput{
				SourceExcerptID: input.SourceExcerptID, ClipID: input.ClipID,
				TrackID: input.TrackID, LocalPrefix: input.LocalPrefix,
			})
		if err != nil {
			return nil, editError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, preview, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("project", input.ProjectID.String(), preview.BaseProjectRevision),
				commandReceiptRef("sequence", input.SequenceID.String(), 0),
			}, preview.BaseProjectRevision, preview.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &captionDeriveHTTPOutput{Body: preview}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-narrative-subtree", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/narratives/{documentId}/subtree",
		Summary: "Show one bounded authored-text Narrative subtree", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "narrative", "show"),
		Extensions:  commandExtensions("narrative", "show"),
	}, func(ctx context.Context, input *narrativeShowHTTPInput) (*narrativeShowHTTPOutput, error) {
		page, err := reads.NarrativeSubtree(
			ctx, input.ProjectID, input.DocumentID, input.ParentID, input.After, input.Limit,
		)
		if err != nil {
			return nil, editError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, page, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("narrative-document", page.DocumentID.String(), page.DocumentRevision),
				commandReceiptRef("narrative-node", page.Parent.ID.String(), page.Parent.Revision),
			}, 0, page.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &narrativeShowHTTPOutput{Body: page}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-sequence-window", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/sequences/{sequenceId}/window",
		Summary: "Show one bounded Sequence time window", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "sequence", "show"),
		Extensions:  commandExtensions("sequence", "show"),
	}, func(ctx context.Context, input *sequenceShowHTTPInput) (*sequenceShowHTTPOutput, error) {
		startValue, parseErr := strconv.ParseInt(input.StartValue, 10, 64)
		if parseErr != nil || strconv.FormatInt(startValue, 10) != input.StartValue {
			return nil, editError(application.ErrEditInvalid)
		}
		durationValue, parseErr := strconv.ParseInt(input.DurationValue, 10, 64)
		if parseErr != nil || strconv.FormatInt(durationValue, 10) != input.DurationValue {
			return nil, editError(application.ErrEditInvalid)
		}
		start, err := domain.NewRationalTime(startValue, input.StartScale)
		if err != nil {
			return nil, editError(application.ErrEditInvalid)
		}
		duration, err := domain.NewRationalTime(durationValue, input.DurationScale)
		if err != nil {
			return nil, editError(application.ErrEditInvalid)
		}
		rangeValue, err := domain.NewTimeRange(start, duration)
		if err != nil {
			return nil, editError(application.ErrEditInvalid)
		}
		var trackID *domain.TrackID
		if input.TrackID != "" {
			parsed, parseErr := domain.ParseTrackID(input.TrackID)
			if parseErr != nil {
				return nil, editError(application.ErrEditInvalid)
			}
			trackID = &parsed
		}
		page, err := reads.SequenceWindow(
			ctx, input.ProjectID, input.SequenceID, trackID, rangeValue, input.After, input.Limit,
		)
		if err != nil {
			return nil, editError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, page, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("sequence", page.SequenceID.String(), page.SequenceRevision),
			}, 0, page.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &sequenceShowHTTPOutput{Body: page}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-edit-entity", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/entities/{kind}/{id}",
		Summary: "Show one editable entity with its exact revision", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "entity", "show"),
		Extensions:  commandExtensions("entity", "show"),
	}, func(ctx context.Context, input *entityShowHTTPInput) (*entityShowHTTPOutput, error) {
		detail, err := reads.Entity(ctx, input.ProjectID, input.Kind, input.ID)
		if err != nil {
			return nil, editError(err)
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, detail, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{commandReceiptRef(string(input.Kind), input.ID, 0)},
			0, detail.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &entityShowHTTPOutput{Body: detail}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "show-edit-proposal", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/edit/proposals/{proposalId}",
		Summary: "Show one durable Edit Proposal", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "edit", "show"),
		Extensions:  commandExtensions("edit", "show"),
	}, func(ctx context.Context, input *editShowHTTPInput) (*editShowHTTPOutput, error) {
		proposal, cursor, err := reads.Proposal(ctx, input.ProjectID, input.ProposalID)
		if err != nil {
			return nil, editError(err)
		}
		result := command.EditShowData{Proposal: proposal, ActivityCursor: cursor}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, result, application.CommandReceiptSucceeded,
			[]application.CommandReceiptRef{
				commandReceiptRef("edit-proposal", proposal.ID.String(), 0),
			}, proposal.BaseProjectRevision, cursor,
		); err != nil {
			return nil, editError(err)
		}
		return &editShowHTTPOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-edit-transactions", Method: http.MethodGet,
		Path:    "/v1/projects/{projectId}/edit/transactions",
		Summary: "List bounded committed Edit history", Tags: []string{"editing"},
		Middlewares: requireCommandAuthority(api, runs, authorizer, "edit", "history"),
		Extensions:  commandExtensions("edit", "history"),
	}, func(ctx context.Context, input *editHistoryHTTPInput) (*editHistoryHTTPOutput, error) {
		after, err := parseEditHistoryRevision(input.After)
		if err != nil {
			return nil, editError(err)
		}
		page, err := reads.History(ctx, input.ProjectID, after, input.Limit)
		if err != nil {
			return nil, editError(err)
		}
		refs := make([]application.CommandReceiptRef, 0, len(page.Transactions))
		for _, transaction := range page.Transactions {
			refs = append(refs, commandReceiptRef(
				"edit-transaction", transaction.ID.String(), transaction.CommittedProjectRevision,
			))
		}
		if err := recordCommandEvidence(
			ctx, runs, input.ProjectID, page, application.CommandReceiptSucceeded,
			refs, 0, page.ActivityCursor,
		); err != nil {
			return nil, editError(err)
		}
		return &editHistoryHTTPOutput{Body: page}, nil
	})
}

func parseEditHistoryRevision(value string) (domain.Revision, error) {
	if value == "" {
		return 0, nil
	}
	var revision domain.Revision
	if err := revision.UnmarshalText([]byte(value)); err != nil {
		return 0, application.ErrEditInvalid
	}
	return revision, nil
}
