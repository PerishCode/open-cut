package release

import (
	"testing"
	"time"

	"github.com/PerishCode/open-cut/utils/target"
)

func TestManifestValidatesOpaqueEntries(t *testing.T) {
	manifest := Manifest{
		Schema: ManifestSchema, Channel: "beta", Version: "1.0.0-beta.1",
		Platform: target.Host().Platform, Arch: target.Host().Arch,
		Launcher: Entry{Entry: "launcher/launcher"}, Payload: Entry{Entry: "payload/app"},
		MinimumBootstrapProtocol: "bootstrap.v1", PublishedAt: time.Now(),
	}
	if err := manifest.ValidateHost("beta", "bootstrap.v1"); err != nil {
		t.Fatal(err)
	}
	manifest.Payload.Entry = "payload/../launcher/launcher"
	if err := manifest.Validate(); err == nil {
		t.Fatal("traversing payload entry validated")
	}
}
