package productcli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
)

func TestProductStatusCLIUsesOnlyTheStableBusinessCommandPath(t *testing.T) {
	invocation, err := parseBusinessInvocation(
		[]string{"product", "status"}, bytes.NewReader(nil), &bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.name != "product status" || invocation.method != "GET" ||
		invocation.path != "/v1/product/status" || invocation.query != "" || len(invocation.body) != 0 {
		t.Fatalf("invocation=%+v", invocation)
	}
	snapshot := application.ProductStatusSnapshot{
		Schema: application.ProductStatusSchema,
		Features: []application.ProductFeatureAvailability{
			{Feature: application.FeatureAssetFrameInspection, State: application.ProductFeatureAvailable},
			{Feature: application.FeatureSequencePreview, State: application.ProductFeatureUnavailable, Reason: application.ProductFeatureNotQualified},
			{Feature: application.FeatureSequenceExport, State: application.ProductFeatureUnavailable, Reason: application.ProductFeatureNotQualified},
			{Feature: application.FeatureSourcePreview, State: application.ProductFeatureAvailable},
			{Feature: application.FeatureLocalTranscription, State: application.ProductFeatureUnavailable, Reason: application.ProductFeatureNotInstalled},
		},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, revision, cursor, err := validateBusinessResponse("product status", encoded); err != nil ||
		revision != nil || cursor != nil {
		t.Fatalf("validation revision=%v cursor=%v err=%v", revision, cursor, err)
	}
}
