package application

import (
	"context"
	"errors"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestProductStatusIsTransientAuthorizedAndClosed(t *testing.T) {
	snapshot := ProductStatusSnapshot{
		Schema: ProductStatusSchema,
		Features: []ProductFeatureAvailability{
			{Feature: FeatureAssetFrameInspection, State: ProductFeatureAvailable},
			{Feature: FeatureSequencePreview, State: ProductFeatureUnavailable, Reason: ProductFeatureNotQualified},
			{Feature: FeatureSequenceExport, State: ProductFeatureUnavailable, Reason: ProductFeatureNotQualified},
			{Feature: FeatureSourcePreview, State: ProductFeatureAvailable},
			{Feature: FeatureLocalTranscription, State: ProductFeatureUnavailable, Reason: ProductFeatureNotInstalled},
		},
	}
	status, err := NewProductStatus(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := status.Read(context.Background()); !errors.Is(err, ErrAuthorityMissing) {
		t.Fatalf("unauthorized read error = %v", err)
	}
	creator, _ := domain.ParseCreatorID("018f0000-0000-7000-8000-000000000201")
	ctx, err := ContextWithAuthority(context.Background(), Authority{
		Surface: AuthorityFirstPartyUI, InstallationID: "installation-test",
		Actor: domain.CreatorActor(creator),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := status.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first.Features[0].State = ProductFeatureUnavailable
	second, err := status.Read(ctx)
	if err != nil || second.Features[0].State != ProductFeatureAvailable {
		t.Fatalf("product status leaked mutable state: %+v err=%v", second, err)
	}
}

func TestProductStatusRejectsIncompleteOrAmbiguousAvailability(t *testing.T) {
	for _, snapshot := range []ProductStatusSnapshot{
		{Schema: ProductStatusSchema},
		{
			Schema: ProductStatusSchema,
			Features: []ProductFeatureAvailability{
				{Feature: FeatureAssetFrameInspection, State: ProductFeatureAvailable, Reason: ProductFeatureNotInstalled},
				{Feature: FeatureSequencePreview, State: ProductFeatureAvailable},
				{Feature: FeatureSequenceExport, State: ProductFeatureAvailable},
				{Feature: FeatureSourcePreview, State: ProductFeatureAvailable},
				{Feature: FeatureLocalTranscription, State: ProductFeatureAvailable},
			},
		},
	} {
		if _, err := NewProductStatus(snapshot); !errors.Is(err, ErrProductStatusInvalid) {
			t.Fatalf("invalid snapshot accepted: %+v err=%v", snapshot, err)
		}
	}
}
