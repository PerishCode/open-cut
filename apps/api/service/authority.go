package service

import (
	"context"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
)

var ErrUnauthorized = errors.New("product request is unauthorized")

type AuthorizationRequest struct {
	Method             string
	Route              string
	Path               string
	Query              string
	BodyDigest         string
	Command            string
	CommandFingerprint string
	RequiredScope      string
	UISession          string
	CLIGrant           string
	CLIChallenge       string
	CLISignature       string
}

type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) (application.Authority, error)
}

type RejectAuthorizer struct{}

func (RejectAuthorizer) Authorize(context.Context, AuthorizationRequest) (application.Authority, error) {
	return application.Authority{}, ErrUnauthorized
}

func (RejectAuthorizer) ChallengeUI(context.Context, UIChallengeRequest) (UIChallengeResult, error) {
	return UIChallengeResult{}, ErrUnauthorized
}

func (RejectAuthorizer) ExchangeUI(context.Context, UISessionRequest) (UISessionResult, error) {
	return UISessionResult{}, ErrUnauthorized
}

func (RejectAuthorizer) ChallengeCLI(context.Context, CLIChallengeRequest) (CLIChallengeResult, error) {
	return CLIChallengeResult{}, ErrUnauthorized
}

func (RejectAuthorizer) ListCLIGrants(context.Context) ([]application.CLIGrant, error) {
	return nil, ErrUnauthorized
}

func (RejectAuthorizer) ListCLIGrantScopeUpgrades(context.Context) ([]application.CLIGrantScopeUpgrade, error) {
	return nil, ErrUnauthorized
}

func (RejectAuthorizer) DecideCLIGrant(context.Context, string, bool) (application.CLIGrant, error) {
	return application.CLIGrant{}, ErrUnauthorized
}

func (RejectAuthorizer) RevokeCLIGrant(context.Context, string) (application.CLIGrant, error) {
	return application.CLIGrant{}, ErrUnauthorized
}

func (RejectAuthorizer) DecideCLIGrantScopeUpgrade(
	context.Context,
	string,
	bool,
) (application.CLIGrantScopeUpgrade, application.CLIGrant, error) {
	return application.CLIGrantScopeUpgrade{}, application.CLIGrant{}, ErrUnauthorized
}
