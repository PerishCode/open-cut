package businessacceptance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	acceptanceCDPEndpointEnvironment     = "OPEN_CUT_ACCEPTANCE_CDP_ENDPOINT"
	acceptanceProductionModelEnvironment = "OPEN_CUT_ACCEPTANCE_PRODUCTION_MODEL"
)

func TestInstalledCreatorToCLI(t *testing.T) {
	if os.Getenv("OPEN_CUT_ACCEPTANCE") != "1" {
		t.Skip("installed business acceptance is opt-in")
	}
	endpoint := os.Getenv(acceptanceCDPEndpointEnvironment)
	if endpoint == "" {
		t.Fatal("installed acceptance requires a loopback Creator CDP endpoint")
	}
	if _, err := exec.LookPath("open-cut"); err != nil {
		t.Fatal("installed acceptance requires only the stable open-cut CLI on PATH")
	}
	productionModel := os.Getenv(acceptanceProductionModelEnvironment) == "1"
	fixture := filepath.Join(t.TempDir(), "acceptance.wav")
	expectedChannels := "2"
	expectedVideo := false
	timeout := 2 * time.Minute
	writeFixture := WriteAudioFixture
	var deliveryPath string
	var nativeSaveDialog NativeSaveDialog
	if productionModel {
		fixture = filepath.Join(t.TempDir(), "acceptance-speech.webm")
		expectedChannels = "1"
		expectedVideo = true
		timeout = 30 * time.Minute
		writeFixture = WriteSpeechFixture
		deliveryPath = filepath.Join(t.TempDir(), "installed-export.webm")
		driver, dialogErr := NewNativeSaveDialog()
		if dialogErr != nil {
			t.Fatal(dialogErr)
		}
		nativeSaveDialog = driver
	}
	if err := writeFixture(fixture); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	observation, err := RunCreatorToCLI(ctx, CreatorToCLIOptions{
		CDPEndpoint:            endpoint,
		ProjectName:            "Installed Creator Acceptance",
		FixturePath:            fixture,
		ExpectedAudioChannels:  expectedChannels,
		ExpectedVideo:          expectedVideo,
		RunIntent:              "Verify installed Creator-to-CLI bootstrap",
		AuthoredText:           "The installed Agent writes through one durable proposal.",
		AcquireProductionModel: productionModel,
		DeliveryPath:           deliveryPath,
		NativeSaveDialog:       nativeSaveDialog,
		CLI:                    InstalledCLI{Environment: ActorEnvironment(os.Environ())},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(observation)
	t.Logf("installed Creator-to-CLI evidence: %s", evidence)
}
