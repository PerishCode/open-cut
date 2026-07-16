package command

import (
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const StatusHeader = "X-Open-Cut-Command-Status"

type Status string

const (
	StatusSucceeded            Status = "succeeded"
	StatusAccepted             Status = "accepted"
	StatusConflict             Status = "conflict"
	StatusStaleTurn            Status = "stale-turn"
	StatusPairingRequired      Status = "pairing-required"
	StatusScopeUpgradeRequired Status = "scope-upgrade-required"
	StatusApprovalRequired     Status = "approval-required"
	StatusWaiting              Status = "waiting"
	StatusNotFound             Status = "not-found"
	StatusUnavailable          Status = "unavailable"
	StatusIncompatible         Status = "incompatible"
	StatusInvalid              Status = "invalid"
	StatusFailed               Status = "failed"
)

type Context = application.InvocationContext

type Issue struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	EntityID string `json:"entityId,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Error struct {
	Code   string  `json:"code"`
	Issues []Issue `json:"issues,omitempty" maxItems:"256"`
}

type Result[Data any] struct {
	Schema          string           `json:"schema"`
	CLIVersion      string           `json:"cliVersion"`
	Command         string           `json:"command"`
	Context         Context          `json:"context"`
	Status          Status           `json:"status" enum:"succeeded,accepted,conflict,stale-turn,pairing-required,scope-upgrade-required,approval-required,waiting,not-found,unavailable,incompatible,invalid,failed"`
	Data            *Data            `json:"data,omitempty"`
	Error           *Error           `json:"error,omitempty"`
	ProjectRevision *domain.Revision `json:"projectRevision,omitempty"`
	ActivityCursor  *domain.Cursor   `json:"activityCursor,omitempty"`
}
