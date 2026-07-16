package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/danielgtaylor/huma/v2"
)

const commandReceiptReplayHeader = "X-Open-Cut-Internal-Receipt-Replay"

type bufferedCommandContext interface {
	huma.Context
}

type bufferedCommandResponse struct {
	bufferedCommandContext
	status  int
	headers http.Header
	body    bytes.Buffer
}

func newBufferedCommandResponse(ctx huma.Context) *bufferedCommandResponse {
	return &bufferedCommandResponse{bufferedCommandContext: ctx, headers: make(http.Header)}
}

func (response *bufferedCommandResponse) SetStatus(code int) {
	response.status = code
}

func (response *bufferedCommandResponse) Status() int {
	return response.status
}

func (response *bufferedCommandResponse) SetHeader(name, value string) {
	response.headers.Set(name, value)
}

func (response *bufferedCommandResponse) AppendHeader(name, value string) {
	response.headers.Add(name, value)
}

func (response *bufferedCommandResponse) BodyWriter() io.Writer {
	return &response.body
}

func (response *bufferedCommandResponse) flush(ctx huma.Context) {
	for name, values := range response.headers {
		if len(values) == 0 {
			continue
		}
		ctx.SetHeader(name, values[0])
		for _, value := range values[1:] {
			ctx.AppendHeader(name, value)
		}
	}
	if response.status != 0 {
		ctx.SetStatus(response.status)
	}
	_, _ = ctx.BodyWriter().Write(response.body.Bytes())
}

func recordCommandBusinessFailures(
	api huma.API,
	runs *application.AgentRuns,
	descriptor command.Descriptor,
) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		response := newBufferedCommandResponse(ctx)
		next(response)
		if response.headers.Get(commandReceiptReplayHeader) == "1" {
			response.headers.Del(commandReceiptReplayHeader)
			response.flush(ctx)
			return
		}
		status, ok := receiptBusinessStatus(response.headers.Get(command.StatusHeader), descriptor)
		if !ok {
			response.flush(ctx)
			return
		}
		authority, err := application.AuthorityFromContext(response.Context())
		if err != nil || authority.Surface != application.AuthorityProductCLI || authority.Invocation == nil ||
			authority.Invocation.Context.ProjectID == nil || authority.Invocation.Context.RunID == nil ||
			authority.Invocation.Context.TurnID == nil {
			response.flush(ctx)
			return
		}
		projectID := *authority.Invocation.Context.ProjectID
		if runs == nil {
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "command result could not be committed")
			return
		}
		_, err = runs.RecordBusinessResult(response.Context(), projectID, application.CommandBusinessResult{
			Status: status,
			Result: struct {
				Status     application.CommandReceiptStatus `json:"status"`
				HTTPStatus int                              `json:"httpStatus"`
			}{Status: status, HTTPStatus: response.status},
		})
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "command result could not be committed")
			return
		}
		response.flush(ctx)
	}
}

func replayPriorCommandFailure(
	ctx context.Context,
	runs *application.AgentRuns,
	projectID domain.ProjectID,
) error {
	if runs == nil {
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("command receipt service is unavailable"))
	}
	receipt, exists, err := runs.PriorCommandReceipt(ctx, projectID)
	if errors.Is(err, application.ErrCommandReceiptRequestReused) {
		return replayedCommandStatusError(
			command.StatusInvalid,
			huma.Error422UnprocessableEntity("command request identity was reused with different input"),
		)
	}
	if err != nil {
		return commandStatusError(command.StatusFailed, huma.Error500InternalServerError("command receipt lookup failed", err))
	}
	if !exists || receipt.Status == application.CommandReceiptSucceeded ||
		receipt.Status == application.CommandReceiptAccepted {
		return nil
	}
	status := command.Status(receipt.Status)
	detail := "command request replays its immutable " + string(status) + " result"
	var result error
	switch receipt.Status {
	case application.CommandReceiptNotFound:
		result = huma.Error404NotFound(detail)
	case application.CommandReceiptUnavailable:
		result = huma.Error503ServiceUnavailable(detail)
	case application.CommandReceiptInvalid:
		result = huma.Error422UnprocessableEntity(detail)
	case application.CommandReceiptFailed:
		result = huma.Error500InternalServerError(detail)
	default:
		result = huma.Error409Conflict(detail)
	}
	return replayedCommandStatusError(status, result)
}

func replayedCommandStatusError(status command.Status, err error) error {
	return huma.ErrorWithHeaders(
		err,
		http.Header{
			command.StatusHeader:       []string{string(status)},
			commandReceiptReplayHeader: []string{"1"},
		},
	)
}

func receiptBusinessStatus(value string, descriptor command.Descriptor) (application.CommandReceiptStatus, bool) {
	status := command.Status(value)
	switch status {
	case command.StatusConflict, command.StatusNotFound, command.StatusUnavailable,
		command.StatusIncompatible, command.StatusInvalid, command.StatusFailed,
		command.StatusWaiting, command.StatusApprovalRequired:
	default:
		return "", false
	}
	for _, allowed := range descriptor.Statuses {
		if allowed == status {
			return application.CommandReceiptStatus(status), true
		}
	}
	return "", false
}

func commandStatusError(status command.Status, err error) error {
	return huma.ErrorWithHeaders(
		err,
		http.Header{command.StatusHeader: []string{string(status)}},
	)
}
