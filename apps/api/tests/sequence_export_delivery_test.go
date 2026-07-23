package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequenceExportDeliveryLeaseIsClientBoundOneShotAndExact(t *testing.T) {
	parallelAPITest(t)
	now := time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC)
	projectID, _ := domain.ParseProjectID("018f0a60-7b80-7a01-8000-000000000301")
	artifactID, _ := domain.ParseArtifactID("018f0a60-7b80-7a01-8000-000000000302")
	content := []byte("immutable-export-delivery")
	hash := sha256.Sum256(content)
	digest, _ := domain.ParseDigest("sha256:" + hex.EncodeToString(hash[:]))
	byteSize, _ := domain.NewUInt64(uint64(len(content)))
	path := filepath.Join(t.TempDir(), "export.webm")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	opener := exportDeliveryFixture{
		path: path,
		media: application.SequenceExportArtifactFile{
			Path: "export.webm", MimeType: "video/webm", ByteSize: byteSize, SHA256: digest,
		},
	}
	delivery, err := service.NewSequenceExportDeliveryService(
		opener, application.ClockFunc(func() time.Time { return now }),
		strings.NewReader(strings.Repeat("a", 512)),
	)
	if err != nil {
		t.Fatal(err)
	}
	issuing, renewed, copied := leaseCreatorRotationContexts(t, now, "export-delivery-issuing")
	lease, err := delivery.Create(issuing, projectID, artifactID)
	if err != nil || lease.ArtifactID != artifactID || lease.ByteLength != byteSize ||
		lease.ContentSHA256 != digest || !strings.Contains(lease.ContentURL, "oc_export_") {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	token := strings.TrimPrefix(lease.ContentURL, "/v1/internal/platform/export-content/")
	renewedResponse := httptest.NewRecorder()
	if err := delivery.ServeContent(renewed, renewedResponse, token); err != nil ||
		renewedResponse.Code != 200 || renewedResponse.Body.String() != string(content) {
		t.Fatalf("rotated session status=%d body=%q err=%v", renewedResponse.Code, renewedResponse.Body.String(), err)
	}

	lease, err = delivery.Create(issuing, projectID, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	token = strings.TrimPrefix(lease.ContentURL, "/v1/internal/platform/export-content/")
	if err := delivery.ServeContent(copied, httptest.NewRecorder(), token); !errors.Is(
		err, service.ErrSequenceExportDeliveryInvalid,
	) {
		t.Fatalf("copied session error=%v", err)
	}
	if err := delivery.ServeContent(issuing, httptest.NewRecorder(), token); !errors.Is(
		err, service.ErrSequenceExportDeliveryInvalid,
	) {
		t.Fatalf("burned copied lease error=%v", err)
	}

	lease, err = delivery.Create(issuing, projectID, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	token = strings.TrimPrefix(lease.ContentURL, "/v1/internal/platform/export-content/")
	response := httptest.NewRecorder()
	if err := delivery.ServeContent(issuing, response, token); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || response.Body.String() != string(content) ||
		response.Header().Get("X-Open-Cut-Content-SHA256") != digest.String() {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if err := delivery.ServeContent(issuing, httptest.NewRecorder(), token); !errors.Is(
		err, service.ErrSequenceExportDeliveryInvalid,
	) {
		t.Fatalf("reused lease error=%v", err)
	}
}

type exportDeliveryFixture struct {
	path  string
	media application.SequenceExportArtifactFile
}

func (fixture exportDeliveryFixture) InspectSequenceExportDelivery(
	context.Context,
	domain.ProjectID,
	domain.ArtifactID,
	time.Time,
) (application.SequenceExportArtifactFile, error) {
	return fixture.media, nil
}

func (fixture exportDeliveryFixture) OpenSequenceExportDelivery(
	context.Context,
	domain.ProjectID,
	domain.ArtifactID,
	time.Time,
) (*os.File, application.SequenceExportArtifactFile, error) {
	file, err := os.Open(fixture.path)
	return file, fixture.media, err
}
