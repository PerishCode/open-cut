package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/PerishCode/open-cut/product/domain"
)

type AuthoritySurface string

const (
	AuthorityFirstPartyUI AuthoritySurface = "first-party-ui"
	AuthorityProductCLI   AuthoritySurface = "product-cli"
)

var (
	ErrAuthorityMissing = errors.New("product authority is missing")
	ErrAuthorityInvalid = errors.New("product authority is invalid")
)

// Authority is the trusted application value produced by an API transport
// verifier. Installation and grant identities authenticate the caller; Actor
// is the durable creative identity written to product history.
type Authority struct {
	Surface        AuthoritySurface
	InstallationID string
	GrantID        string
	Actor          domain.ActorRef
	Policy         InvocationPolicy
	Invocation     *CommandInvocation
}

func (authority Authority) Validate() error {
	if _, err := domain.ParseRequestID(authority.InstallationID); err != nil {
		return fmt.Errorf("%w: installation identity", ErrAuthorityInvalid)
	}
	if err := authority.Actor.Validate(); err != nil {
		return fmt.Errorf("%w: creative actor", ErrAuthorityInvalid)
	}
	switch authority.Surface {
	case AuthorityFirstPartyUI:
		if authority.GrantID != "" || authority.Actor.Kind != domain.ActorCreator || authority.Invocation != nil {
			return ErrAuthorityInvalid
		}
	case AuthorityProductCLI:
		if _, err := domain.ParseRequestID(authority.GrantID); err != nil || authority.Actor.Kind != domain.ActorAgent ||
			authority.Invocation == nil || authority.Invocation.Validate() != nil {
			return ErrAuthorityInvalid
		}
	default:
		return ErrAuthorityInvalid
	}
	if authority.Policy != (InvocationPolicy{}) {
		if err := authority.Policy.Validate(); err != nil {
			return fmt.Errorf("%w: invocation policy", ErrAuthorityInvalid)
		}
	}
	return nil
}

func (authority Authority) EffectivePolicy() InvocationPolicy {
	if authority.Policy == (InvocationPolicy{}) {
		return DefaultInvocationPolicy()
	}
	return authority.Policy
}

type authorityContextKey struct{}

func ContextWithAuthority(ctx context.Context, authority Authority) (context.Context, error) {
	if err := authority.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, authorityContextKey{}, authority), nil
}

func AuthorityFromContext(ctx context.Context) (Authority, error) {
	authority, ok := ctx.Value(authorityContextKey{}).(Authority)
	if !ok {
		return Authority{}, ErrAuthorityMissing
	}
	if err := authority.Validate(); err != nil {
		return Authority{}, err
	}
	return authority, nil
}
