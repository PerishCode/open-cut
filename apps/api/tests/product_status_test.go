package tests

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/product/application"
)

func TestProductStatusSeparatesAbsentInvalidAndUnqualifiedMediaTools(t *testing.T) {
	tests := []struct {
		name     string
		verified mediatoolchain.Verified
		err      error
		reason   application.ProductFeatureUnavailableReason
	}{
		{name: "absent", err: mediatoolchain.ErrUnavailable, reason: application.ProductFeatureNotInstalled},
		{name: "invalid", err: errors.New("digest mismatch"), reason: application.ProductFeatureInvalid},
		{
			name: "unqualified", verified: mediatoolchain.Verified{
				Capabilities: map[string]mediatoolchain.Capability{},
			}, reason: application.ProductFeatureNotQualified,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, err := service.NewProductStatusFromMediaTools(test.verified, test.err)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := status.Read(creatorContext(t))
			if err != nil {
				t.Fatal(err)
			}
			for index, feature := range snapshot.Features {
				expected := test.reason
				if index == 4 && test.err == nil {
					expected = application.ProductFeatureNotInstalled
				}
				if feature.State != application.ProductFeatureUnavailable || feature.Reason != expected {
					t.Fatalf("feature=%+v", feature)
				}
			}
		})
	}
}

func TestProductStatusHTTPIsAuthorizedAndDoesNotExposeToolDetails(t *testing.T) {
	store := repository.NewMemoryProjects()
	projects, reads, activity, runs := testProjectApplications(t, store)
	edits, editReads := testEditingApplications(t, store)
	media, assetReads, sourceAccess := testMediaApplications(t, store)
	status, err := service.NewProductStatusFromMediaTools(
		mediatoolchain.Verified{Capabilities: map[string]mediatoolchain.Capability{}}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}), status, nil,
		projects, reads, activity, runs, edits, editReads, media, assetReads, sourceAccess,
		nil, nil, nil, nil, nil, creatorAuthorizer{},
	)
	request := httptest.NewRequest(http.MethodGet, "/v1/product/status", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	encoded := response.Body.String()
	var snapshot application.ProductStatusSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"path", "sha256", "catalog", "sidecar", "capability"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("product status exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestProductStatusMapsOnlyClosedProductFeaturesFromVerifiedTools(t *testing.T) {
	verified := mediatoolchain.Verified{Capabilities: map[string]mediatoolchain.Capability{
		mediatoolchain.CapabilityProbeV1:       {},
		mediatoolchain.CapabilityFrameRGBV1:    {},
		mediatoolchain.CapabilitySourceProxyV1: {},
	}}
	status, err := service.NewProductStatusFromMediaTools(verified, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := status.Read(creatorContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Features[0].State != application.ProductFeatureAvailable ||
		snapshot.Features[1].Reason != application.ProductFeatureNotQualified ||
		snapshot.Features[2].Reason != application.ProductFeatureNotQualified ||
		snapshot.Features[3].State != application.ProductFeatureAvailable {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestProductStatusRequiresBothTranscriptionExecutorAndAuthenticatedModelDeclaration(t *testing.T) {
	entry := resourceCatalogEntry(t, "https://catalog.invalid/whisper-small.bin", []byte("model"))
	verified := mediatoolchain.Verified{Capabilities: map[string]mediatoolchain.Capability{
		mediatoolchain.CapabilityLocalTranscriptionV1: {},
	}}
	status, err := service.NewProductStatusFromClosures(
		verified, nil, productresource.Verified{Entries: []application.ProductResourceCatalogEntry{entry}}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := status.Read(creatorContext(t))
	if err != nil {
		t.Fatal(err)
	}
	transcription := snapshot.Features[4]
	if transcription.Feature != application.FeatureLocalTranscription ||
		transcription.State != application.ProductFeatureAvailable || transcription.Reason != "" {
		t.Fatalf("local transcription=%+v", transcription)
	}
}
