package application

import (
	"context"
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestAuthoritySeparatesAuthenticationPrincipalFromCreativeActor(t *testing.T) {
	creatorID, _ := domain.ParseCreatorID("018f0000-0000-7000-8000-000000000101")
	authority := Authority{
		Surface: AuthorityFirstPartyUI, InstallationID: "installation-test",
		Actor: domain.CreatorActor(creatorID),
	}
	ctx, err := ContextWithAuthority(context.Background(), authority)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := AuthorityFromContext(ctx)
	if err != nil || loaded.InstallationID != authority.InstallationID || loaded.Actor.IDString() != creatorID.String() {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if _, err := AuthorityFromContext(context.Background()); !errors.Is(err, ErrAuthorityMissing) {
		t.Fatalf("missing authority error = %v", err)
	}
	invalid := authority
	invalid.Surface = AuthorityProductCLI
	if _, err := ContextWithAuthority(context.Background(), invalid); !errors.Is(err, ErrAuthorityInvalid) {
		t.Fatalf("invalid authority error = %v", err)
	}
}
