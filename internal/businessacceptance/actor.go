package businessacceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

const (
	helpSchema    = "open-cut/cli-help/v1"
	commandSchema = "open-cut/command/v1"
)

type CommandOutput struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type CommandExecutor interface {
	Execute(context.Context, []byte, ...string) (CommandOutput, error)
}

type InstalledCLI struct {
	Environment []string
}

func (cli InstalledCLI) Execute(ctx context.Context, input []byte, arguments ...string) (CommandOutput, error) {
	command := exec.CommandContext(ctx, "open-cut", arguments...)
	command.Env = cli.Environment
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return CommandOutput{}, err
		}
		exitCode = exit.ExitCode()
	}
	return CommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode}, nil
}

type Actor struct {
	CLI CommandExecutor
}

type PairingRequest struct {
	ID string
}

type Observation struct {
	ProjectID                  string              `json:"projectId"`
	ProjectRevision            string              `json:"projectRevision"`
	SequenceID                 string              `json:"sequenceId"`
	SequenceRevision           string              `json:"sequenceRevision"`
	VideoTrackID               string              `json:"videoTrackId"`
	VideoTrackRevision         string              `json:"videoTrackRevision"`
	AudioTrackID               string              `json:"audioTrackId"`
	AudioTrackRevision         string              `json:"audioTrackRevision"`
	NarrativeDocument          string              `json:"narrativeDocumentId"`
	NarrativeRoot              string              `json:"narrativeRootNodeId"`
	AssetID                    string              `json:"assetId"`
	AssetState                 string              `json:"assetState"`
	AssetFingerprint           string              `json:"assetFingerprint,omitempty"`
	MediaContainer             string              `json:"mediaContainer,omitempty"`
	MediaStreamID              string              `json:"mediaStreamId,omitempty"`
	VideoStreamID              string              `json:"videoStreamId,omitempty"`
	MediaStreamType            string              `json:"mediaStreamType,omitempty"`
	MediaSampleRate            string              `json:"mediaSampleRate,omitempty"`
	MediaChannels              string              `json:"mediaChannels,omitempty"`
	FactsArtifactID            string              `json:"factsArtifactId,omitempty"`
	ProxyArtifactID            string              `json:"proxyArtifactId,omitempty"`
	IdentifyJobState           string              `json:"identifyJobState,omitempty"`
	ProbeJobState              string              `json:"probeJobState,omitempty"`
	ProxyJobState              string              `json:"proxyJobState,omitempty"`
	WaveformJobState           string              `json:"waveformJobState,omitempty"`
	TranscriptJobState         string              `json:"transcriptJobState,omitempty"`
	TranscriptArtifact         string              `json:"transcriptArtifactId,omitempty"`
	TranscriptSegment          string              `json:"transcriptSegmentId,omitempty"`
	TranscriptSegmentIDs       []string            `json:"transcriptSegmentIds,omitempty"`
	TranscriptSegments         int                 `json:"transcriptSegmentCount,omitempty"`
	TranscriptTokens           int                 `json:"transcriptTokenCount,omitempty"`
	TranscriptLanguage         string              `json:"transcriptLanguage,omitempty"`
	TranscriptModel            string              `json:"transcriptModelVersion,omitempty"`
	TranscriptRead             string              `json:"transcriptReadStatus,omitempty"`
	TranscriptText             string              `json:"transcriptText,omitempty"`
	TranscriptSourceRange      *ExactRangeEvidence `json:"transcriptSourceRange,omitempty"`
	RunID                      string              `json:"runId"`
	TurnID                     string              `json:"turnId"`
	RunStatus                  string              `json:"runStatus"`
	ProposalID                 string              `json:"proposalId,omitempty"`
	TransactionID              string              `json:"transactionId,omitempty"`
	AuthoredTextID             string              `json:"authoredTextId,omitempty"`
	EditStatus                 string              `json:"editStatus,omitempty"`
	SourceExcerptID            string              `json:"sourceExcerptId,omitempty"`
	SourceExcerptRevision      string              `json:"sourceExcerptRevision,omitempty"`
	SourceExcerptEvidence      string              `json:"sourceExcerptEvidenceStatus,omitempty"`
	SourceExcerptProposalID    string              `json:"sourceExcerptProposalId,omitempty"`
	SourceExcerptTransactionID string              `json:"sourceExcerptTransactionId,omitempty"`
	RoughCutProposalID         string              `json:"roughCutProposalId,omitempty"`
	RoughCutTransactionID      string              `json:"roughCutTransactionId,omitempty"`
	RoughCutClipID             string              `json:"roughCutClipId,omitempty"`
	RoughCutVideoClipID        string              `json:"roughCutVideoClipId,omitempty"`
	RoughCutLinkGroupID        string              `json:"roughCutLinkGroupId,omitempty"`
	RoughCutAlignmentID        string              `json:"roughCutAlignmentId,omitempty"`
	RoughCutStatus             string              `json:"roughCutStatus,omitempty"`
	SequenceFrameJobID         string              `json:"sequenceFrameJobId,omitempty"`
	SequenceFrameStatus        string              `json:"sequenceFrameStatus,omitempty"`
	SequenceFrameProfile       string              `json:"sequenceFrameProfile,omitempty"`
	SequenceFrameResourceIDs   []string            `json:"sequenceFrameResourceIds,omitempty"`
	SequenceFrameOrdinals      []string            `json:"sequenceFrameOrdinals,omitempty"`
	ExportJobID                string              `json:"exportJobId,omitempty"`
	ExportRootJobID            string              `json:"exportRootJobId,omitempty"`
	ExportArtifactID           string              `json:"exportArtifactId,omitempty"`
	ExportStatus               string              `json:"exportStatus,omitempty"`
	ExportPreset               string              `json:"exportPreset,omitempty"`
	ExportVerification         string              `json:"exportVerification,omitempty"`
	ExportContentDigest        string              `json:"exportContentDigest,omitempty"`
	ExportByteSize             string              `json:"exportByteSize,omitempty"`
	ExportDeliveryStatus       string              `json:"exportDeliveryStatus,omitempty"`
	ExportDeliveryName         string              `json:"exportDeliveryName,omitempty"`
}

type ExactTimeEvidence struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

type ExactRangeEvidence struct {
	Start    ExactTimeEvidence `json:"start"`
	Duration ExactTimeEvidence `json:"duration"`
}

func (actor Actor) DiscoverAndRequestPairing(ctx context.Context) (PairingRequest, error) {
	if actor.CLI == nil {
		return PairingRequest{}, fmt.Errorf("business actor requires the installed CLI")
	}
	root, err := actor.help(ctx, "--help")
	if err != nil {
		return PairingRequest{}, err
	}
	if !hasChild(root, "project", false) || !hasChild(root, "asset", false) || !hasChild(root, "run", false) {
		return PairingRequest{}, fmt.Errorf("root discovery does not expose project, asset, and run command groups")
	}
	project, err := actor.help(ctx, "project", "--help")
	if err != nil {
		return PairingRequest{}, err
	}
	if !hasChild(project, "list", true) || !hasChild(project, "show", true) {
		return PairingRequest{}, fmt.Errorf("project discovery is incomplete")
	}
	leaf, err := actor.help(ctx, "project", "list", "--help")
	if err != nil {
		return PairingRequest{}, err
	}
	if leaf["fingerprint"] == "" || leaf["input"] == nil || leaf["result"] == nil {
		return PairingRequest{}, fmt.Errorf("project list discovery omits its executable schema")
	}
	result, err := actor.command(ctx, "project", "list")
	if err != nil {
		return PairingRequest{}, err
	}
	if result.status != "pairing-required" {
		return PairingRequest{}, fmt.Errorf("first business command status = %q, want pairing-required", result.status)
	}
	pairingID := issueEntityID(result.raw)
	if pairingID == "" {
		return PairingRequest{}, fmt.Errorf("pairing-required result omitted the pending authority identity")
	}
	return PairingRequest{ID: pairingID}, nil
}

func (actor Actor) BeginAndObserveStandaloneRun(
	ctx context.Context,
	projectID, intent string,
) (Observation, error) {
	if projectID == "" || intent == "" {
		return Observation{}, fmt.Errorf("standalone AgentRun input is incomplete")
	}
	group, err := actor.help(ctx, "run", "--help")
	if err != nil {
		return Observation{}, err
	}
	if !hasChild(group, "begin", true) || !hasChild(group, "show", true) {
		return Observation{}, fmt.Errorf("run discovery is incomplete")
	}
	for _, leaf := range []string{"begin", "show"} {
		discovery, discoveryErr := actor.help(ctx, "run", leaf, "--help")
		if discoveryErr != nil {
			return Observation{}, discoveryErr
		}
		if discovery["fingerprint"] == "" || discovery["input"] == nil || discovery["result"] == nil {
			return Observation{}, fmt.Errorf("run %s discovery omits its executable schema", leaf)
		}
	}
	begin, err := actor.command(
		ctx, "run", "begin", "--project-id", projectID,
		"--request-id", "installed-acceptance.run-begin.v1", "--intent", intent,
	)
	if err != nil {
		return Observation{}, err
	}
	if begin.status != "succeeded" {
		return Observation{}, fmt.Errorf("run begin status = %q", begin.status)
	}
	observation, err := observeRun(begin.data, projectID)
	if err != nil {
		return Observation{}, err
	}
	shown, err := actor.command(
		ctx, "run", "show", "--project-id", projectID, "--run-id", observation.RunID,
	)
	if err != nil {
		return Observation{}, err
	}
	if shown.status != "succeeded" {
		return Observation{}, fmt.Errorf("run show status = %q", shown.status)
	}
	confirmed, err := observeRun(shown.data, projectID)
	if err != nil {
		return Observation{}, err
	}
	if confirmed.ProjectID != observation.ProjectID || confirmed.RunID != observation.RunID ||
		confirmed.TurnID != observation.TurnID || confirmed.RunStatus != observation.RunStatus {
		return Observation{}, fmt.Errorf("run show did not confirm the begun AgentRun")
	}
	return observation, nil
}

func (actor Actor) ObserveCreatorBootstrap(
	ctx context.Context,
	projectName, assetName string,
) (Observation, error) {
	projects, err := actor.command(ctx, "project", "list")
	if err != nil {
		return Observation{}, err
	}
	if projects.status != "succeeded" {
		return Observation{}, fmt.Errorf("authorized project list status = %q", projects.status)
	}
	projectID := findNamedID(projects.data, "projects", projectName, "name")
	if projectID == "" {
		return Observation{}, fmt.Errorf("Creator project %q is absent from the CLI projection", projectName)
	}
	overview, err := actor.command(ctx, "project", "show", "--project-id", projectID)
	if err != nil {
		return Observation{}, err
	}
	overviewData := record(overview.data)
	project := record(overviewData["project"])
	sequenceID, _ := project["mainSequenceId"].(string)
	sequenceRevision, _ := overviewData["mainSequenceRevision"].(string)
	projectRevision, _ := project["revision"].(string)
	documentID, _ := project["narrativeDocumentId"].(string)
	rootID, _ := overviewData["narrativeRootNodeId"].(string)
	videoTrackID, videoTrackRevision := findTrack(overviewData["tracks"], "video")
	audioTrackID, audioTrackRevision := findTrack(overviewData["tracks"], "audio")
	if overview.status != "succeeded" || sequenceID == "" || sequenceRevision == "" || projectRevision == "" ||
		documentID == "" || rootID == "" || videoTrackID == "" || videoTrackRevision == "" ||
		audioTrackID == "" || audioTrackRevision == "" {
		return Observation{}, fmt.Errorf("project show did not return the main Sequence identity")
	}
	assets, err := actor.command(ctx, "asset", "list", "--project-id", projectID)
	if err != nil {
		return Observation{}, err
	}
	if assets.status != "succeeded" {
		return Observation{}, fmt.Errorf("asset list status = %q", assets.status)
	}
	assetID, assetState := findAsset(assets.data, assetName)
	if assetID == "" {
		return Observation{}, fmt.Errorf("Creator Asset %q is absent from the CLI projection", assetName)
	}
	return Observation{
		ProjectID: projectID, ProjectRevision: projectRevision, SequenceID: sequenceID,
		SequenceRevision: sequenceRevision,
		VideoTrackID:     videoTrackID, VideoTrackRevision: videoTrackRevision,
		AudioTrackID: audioTrackID, AudioTrackRevision: audioTrackRevision,
		NarrativeDocument: documentID, NarrativeRoot: rootID,
		AssetID: assetID, AssetState: assetState,
	}, nil
}

func findTrack(value any, expectedType string) (string, string) {
	for _, item := range list(value) {
		track := record(item)
		if track["type"] != expectedType {
			continue
		}
		id, _ := track["id"].(string)
		revision, _ := track["revision"].(string)
		return id, revision
	}
	return "", ""
}

type commandResult struct {
	status string
	data   any
	raw    map[string]any
}

func (actor Actor) help(ctx context.Context, arguments ...string) (map[string]any, error) {
	output, err := actor.CLI.Execute(ctx, nil, arguments...)
	if err != nil {
		return nil, err
	}
	if output.ExitCode != 0 || len(bytes.TrimSpace(output.Stderr)) != 0 {
		return nil, fmt.Errorf("help %v exited %d: %s", arguments, output.ExitCode, bytes.TrimSpace(output.Stderr))
	}
	value, err := oneJSONObject(output.Stdout)
	if err != nil {
		return nil, err
	}
	if value["schema"] != helpSchema {
		return nil, fmt.Errorf("help %v schema = %v", arguments, value["schema"])
	}
	return value, nil
}

func (actor Actor) command(ctx context.Context, arguments ...string) (commandResult, error) {
	return actor.commandInput(ctx, nil, arguments...)
}

func (actor Actor) commandInput(ctx context.Context, input []byte, arguments ...string) (commandResult, error) {
	output, err := actor.CLI.Execute(ctx, input, arguments...)
	if err != nil {
		return commandResult{}, err
	}
	if output.ExitCode != 0 {
		return commandResult{}, fmt.Errorf("command %v exited %d: %s", arguments, output.ExitCode, bytes.TrimSpace(output.Stderr))
	}
	value, err := oneJSONObject(output.Stdout)
	if err != nil {
		return commandResult{}, err
	}
	if value["schema"] != commandSchema {
		return commandResult{}, fmt.Errorf("command %v schema = %v", arguments, value["schema"])
	}
	status, _ := value["status"].(string)
	return commandResult{status: status, data: value["data"], raw: value}, nil
}

func oneJSONObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode CLI JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("CLI emitted trailing output")
	}
	return value, nil
}

func hasChild(help map[string]any, name string, leaf bool) bool {
	for _, value := range list(help["children"]) {
		child := record(value)
		if child["name"] == name && child["leaf"] == leaf {
			return true
		}
	}
	return false
}

func issueEntityID(result map[string]any) string {
	issues := list(record(result["error"])["issues"])
	if len(issues) == 0 {
		return ""
	}
	id, _ := record(issues[0])["entityId"].(string)
	return id
}

func findNamedID(data any, collection, expected, nameField string) string {
	for _, value := range list(record(data)[collection]) {
		candidate := record(value)
		if candidate[nameField] == expected {
			id, _ := candidate["id"].(string)
			return id
		}
	}
	return ""
}

func findAsset(data any, expected string) (string, string) {
	for _, value := range list(record(data)["assets"]) {
		asset := record(value)
		if asset["displayName"] == expected {
			id, _ := asset["id"].(string)
			state, _ := asset["availability"].(string)
			return id, state
		}
	}
	return "", ""
}

func observeRun(data any, expectedProjectID string) (Observation, error) {
	run := record(record(data)["run"])
	turn := record(run["currentTurn"])
	runID, _ := run["id"].(string)
	projectID, _ := run["projectId"].(string)
	runStatus, _ := run["status"].(string)
	turnID, _ := turn["id"].(string)
	turnStatus, _ := turn["status"].(string)
	generation, _ := turn["generation"].(string)
	if projectID != expectedProjectID || runID == "" || turnID == "" ||
		runStatus != "active" || turnStatus != "active" || generation != "1" {
		return Observation{}, fmt.Errorf("standalone AgentRun projection is incomplete")
	}
	return Observation{ProjectID: projectID, RunID: runID, TurnID: turnID, RunStatus: runStatus}, nil
}

func record(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func list(value any) []any {
	result, _ := value.([]any)
	return result
}
