package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type scriptedCLI struct {
	steps           []scriptedStep
	index           int
	inputValidator  func([]byte) error
	inputValidators []func([]byte) error
}

type scriptedStep struct {
	arguments []string
	value     any
}

func (cli *scriptedCLI) Execute(_ context.Context, input []byte, arguments ...string) (CommandOutput, error) {
	if cli.index >= len(cli.steps) {
		return CommandOutput{}, fmt.Errorf("unexpected invocation %v", arguments)
	}
	step := cli.steps[cli.index]
	cli.index++
	if !reflect.DeepEqual(arguments, step.arguments) {
		return CommandOutput{}, fmt.Errorf("arguments = %v, want %v", arguments, step.arguments)
	}
	if len(input) > 0 {
		validator := cli.inputValidator
		if len(cli.inputValidators) > 0 {
			validator = cli.inputValidators[0]
			cli.inputValidators = cli.inputValidators[1:]
		}
		if validator == nil {
			return CommandOutput{}, fmt.Errorf("unexpected stdin for %v", arguments)
		}
		if err := validator(input); err != nil {
			return CommandOutput{}, err
		}
		if len(cli.inputValidators) == 0 {
			cli.inputValidator = nil
		}
	}
	encoded, err := json.Marshal(step.value)
	return CommandOutput{Stdout: encoded}, err
}

func TestActorDiscoversPairsAndObservesOnlyThroughCLI(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000001"
	sequenceID := "018f0000-0000-7000-8000-000000000002"
	videoTrackID := "018f0000-0000-7000-8000-000000000020"
	audioTrackID := "018f0000-0000-7000-8000-000000000021"
	assetID := "018f0000-0000-7000-8000-000000000003"
	runID := "018f0000-0000-7000-8000-000000000004"
	turnID := "018f0000-0000-7000-8000-000000000005"
	documentID := "018f0000-0000-7000-8000-000000000006"
	rootID := "018f0000-0000-7000-8000-000000000007"
	authoredTextID := "018f0000-0000-7000-8000-000000000008"
	proposalID := "018f0000-0000-7000-8000-000000000009"
	transactionID := "018f0000-0000-7000-8000-00000000000a"
	streamID := "018f0000-0000-7000-8000-00000000000b"
	factsArtifactID := "018f0000-0000-7000-8000-00000000000c"
	proxyArtifactID := "018f0000-0000-7000-8000-00000000000d"
	proposalDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fingerprint := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	authoredText := "Write the installed acceptance line."
	cli := &scriptedCLI{steps: []scriptedStep{
		{[]string{"--help"}, help([]any{child("asset", false), child("project", false), child("run", false)}, nil)},
		{[]string{"project", "--help"}, help([]any{child("list", true), child("show", true)}, nil)},
		{[]string{"project", "list", "--help"}, help(nil, map[string]any{
			"fingerprint": "sha256:test", "input": map[string]any{}, "result": map[string]any{},
		})},
		{[]string{"project", "list"}, result("pairing-required", nil, map[string]any{
			"code": "pairing-required", "issues": []any{map[string]any{"entityId": projectID}},
		})},
		{[]string{"project", "list"}, result("succeeded", map[string]any{
			"projects": []any{map[string]any{"id": projectID, "name": "Acceptance story"}},
		}, nil)},
		{[]string{"project", "show", "--project-id", projectID}, result("succeeded", map[string]any{
			"project": map[string]any{
				"id": projectID, "revision": "1", "mainSequenceId": sequenceID,
				"narrativeDocumentId": documentID,
			},
			"narrativeRootNodeId":  rootID,
			"mainSequenceRevision": "1",
			"tracks": []any{
				map[string]any{"id": videoTrackID, "revision": "1", "type": "video"},
				map[string]any{"id": audioTrackID, "revision": "1", "type": "audio"},
			},
		}, nil)},
		{[]string{"asset", "list", "--project-id", projectID}, result("succeeded", map[string]any{
			"assets": []any{map[string]any{
				"id": assetID, "displayName": "acceptance.wav", "availability": "online",
			}},
		}, nil)},
		{[]string{"run", "--help"}, help([]any{child("begin", true), child("show", true)}, nil)},
		{[]string{"run", "begin", "--help"}, help(nil, map[string]any{
			"fingerprint": "sha256:begin", "input": map[string]any{}, "result": map[string]any{},
		})},
		{[]string{"run", "show", "--help"}, help(nil, map[string]any{
			"fingerprint": "sha256:show", "input": map[string]any{}, "result": map[string]any{},
		})},
		{[]string{
			"run", "begin", "--project-id", projectID,
			"--request-id", "installed-acceptance.run-begin.v1", "--intent", "Create the acceptance cut",
		}, result("succeeded", runData(projectID, runID, turnID), nil)},
		{[]string{"run", "show", "--project-id", projectID, "--run-id", runID},
			result("succeeded", runData(projectID, runID, turnID), nil)},
		{[]string{"asset", "--help"}, help([]any{child("list", true), child("inspect", true)}, nil)},
		{[]string{"asset", "inspect", "--help"}, executableHelp("sha256:asset-inspect")},
		{[]string{"asset", "inspect", "--project-id", projectID, "--asset-id", assetID}, result(
			"succeeded",
			mediaData(projectID, assetID, streamID, factsArtifactID, proxyArtifactID, fingerprint),
			nil,
		)},
		{[]string{"narrative", "--help"}, help([]any{child("show", true)}, nil)},
		{[]string{"narrative", "show", "--help"}, executableHelp("sha256:narrative-show")},
		{[]string{"entity", "--help"}, help([]any{child("show", true)}, nil)},
		{[]string{"entity", "show", "--help"}, executableHelp("sha256:entity-show")},
		{[]string{"edit", "--help"}, help([]any{
			child("propose", true), child("apply", true), child("show", true), child("history", true),
		}, nil)},
		{[]string{"edit", "propose", "--help"}, executableHelp("sha256:edit-propose")},
		{[]string{"edit", "apply", "--help"}, executableHelp("sha256:edit-apply")},
		{[]string{"edit", "show", "--help"}, executableHelp("sha256:edit-show")},
		{[]string{"edit", "history", "--help"}, executableHelp("sha256:edit-history")},
		{[]string{
			"narrative", "show", "--project-id", projectID,
			"--document-id", documentID, "--parent-id", rootID,
		}, result("succeeded", narrativeData(documentID, rootID, "1", nil), nil)},
		{append([]string{"edit", "propose", "--input", "-"},
			acceptanceEditContext(projectID, sequenceID, runID, turnID)...),
			result("succeeded", proposalData(proposalID, proposalDigest, authoredTextID, "open", ""), nil)},
		{append([]string{
			"edit", "apply", "--proposal-id", proposalID,
			"--request-id", "installed-acceptance.edit-apply.v1", "--proposal-digest", proposalDigest,
		}, acceptanceEditContext(projectID, sequenceID, runID, turnID)...), result("succeeded", map[string]any{
			"proposal": proposalData(proposalID, proposalDigest, authoredTextID, "applied", transactionID)["proposal"],
			"transaction": map[string]any{
				"id": transactionID, "committedProjectRevision": "2",
			},
		}, nil)},
		{[]string{
			"narrative", "show", "--project-id", projectID,
			"--document-id", documentID, "--parent-id", rootID,
		}, result("succeeded", narrativeData(documentID, rootID, "2", []any{
			map[string]any{"kind": "authored-text", "authoredText": authoredTextData(authoredTextID, authoredText)},
		}), nil)},
		{[]string{
			"entity", "show", "--project-id", projectID,
			"--kind", "narrative-node", "--id", authoredTextID,
		}, result("succeeded", map[string]any{
			"kind": "narrative-node", "authoredText": authoredTextData(authoredTextID, authoredText),
		}, nil)},
		{[]string{"edit", "show", "--project-id", projectID, "--proposal-id", proposalID},
			result("succeeded", proposalData(proposalID, proposalDigest, authoredTextID, "applied", transactionID), nil)},
		{[]string{"edit", "history", "--project-id", projectID, "--after", "1", "--limit", "10"},
			result("succeeded", map[string]any{"transactions": []any{map[string]any{
				"id": transactionID, "committedProjectRevision": "2",
			}}}, nil)},
	}}
	cli.inputValidator = func(input []byte) error {
		var actual any
		if err := json.Unmarshal(input, &actual); err != nil {
			return err
		}
		expected := map[string]any{
			"requestId": "installed-acceptance.edit-propose.v1",
			"intent":    "Write one installed acceptance line", "baseProjectRevision": "1",
			"preconditions": []any{map[string]any{
				"kind": "narrative-node", "id": rootID, "revision": "1",
			}},
			"operations": []any{map[string]any{
				"type": "insert-authored-text", "createAs": acceptanceAuthoredTextLocal,
				"parentId": rootID, "authoredTextPurpose": "spoken",
				"language": "en", "text": authoredText,
			}},
		}
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("proposal input = %#v, want %#v", actual, expected)
		}
		return nil
	}
	actor := Actor{CLI: cli}
	pairing, err := actor.DiscoverAndRequestPairing(context.Background())
	if err != nil || pairing.ID != projectID {
		t.Fatalf("pairing=%+v error=%v", pairing, err)
	}
	observation, err := actor.ObserveCreatorBootstrap(context.Background(), "Acceptance story", "acceptance.wav")
	if err != nil {
		t.Fatal(err)
	}
	if observation.ProjectID != projectID || observation.ProjectRevision != "1" ||
		observation.SequenceID != sequenceID || observation.SequenceRevision != "1" ||
		observation.NarrativeDocument != documentID ||
		observation.NarrativeRoot != rootID || observation.VideoTrackID != videoTrackID ||
		observation.VideoTrackRevision != "1" || observation.AudioTrackID != audioTrackID ||
		observation.AudioTrackRevision != "1" ||
		observation.AssetID != assetID || observation.AssetState != "online" {
		t.Fatalf("observation=%+v", observation)
	}
	run, err := actor.BeginAndObserveStandaloneRun(context.Background(), projectID, "Create the acceptance cut")
	if err != nil {
		t.Fatal(err)
	}
	if run.ProjectID != projectID || run.RunID != runID || run.TurnID != turnID || run.RunStatus != "active" {
		t.Fatalf("run=%+v", run)
	}
	observation.RunID, observation.TurnID, observation.RunStatus = run.RunID, run.TurnID, run.RunStatus
	observation, err = actor.ObserveMediaPipeline(context.Background(), observation, "2", false)
	if err != nil {
		t.Fatal(err)
	}
	if observation.AssetFingerprint != fingerprint || observation.MediaStreamID != streamID ||
		observation.MediaStreamType != "audio" ||
		observation.FactsArtifactID != factsArtifactID || observation.ProxyArtifactID != proxyArtifactID ||
		observation.IdentifyJobState != "succeeded" || observation.ProbeJobState != "succeeded" ||
		observation.ProxyJobState != "succeeded" || observation.WaveformJobState != "blocked" ||
		observation.TranscriptJobState != "blocked" {
		t.Fatalf("media observation=%+v", observation)
	}
	edited, err := actor.ProposeAndApplyAuthoredText(context.Background(), observation, authoredText)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ProjectRevision != "2" || edited.ProposalID != proposalID ||
		edited.TransactionID != transactionID || edited.AuthoredTextID != authoredTextID ||
		edited.EditStatus != "applied" {
		t.Fatalf("edited=%+v", edited)
	}
}

func mediaData(
	projectID, assetID, streamID, factsArtifactID, proxyArtifactID, fingerprint string,
) map[string]any {
	return map[string]any{"asset": map[string]any{
		"id": assetID, "projectId": projectID, "availability": "online",
		"acceptedFingerprint": fingerprint, "fingerprint": fingerprint,
		"facts": map[string]any{
			"container": "wav", "containerAliases": []any{},
			"streams": []any{map[string]any{
				"id": streamID,
				"descriptor": map[string]any{
					"mediaType": "audio", "codec": "pcm_s16le", "dispositions": []any{},
					"audio": map[string]any{"sampleRate": 48000, "channels": 2},
				},
			}},
		},
		"artifacts": []any{
			map[string]any{
				"id": factsArtifactID, "kind": "media-facts", "state": "ready",
				"inputFingerprint": fingerprint,
			},
			map[string]any{
				"id": proxyArtifactID, "kind": "proxy", "state": "ready",
				"inputFingerprint": fingerprint,
			},
		},
		"jobs": []any{
			mediaJob("identify", "succeeded", "", []any{}),
			mediaJob("probe", "succeeded", factsArtifactID, []any{}),
			mediaJob("proxy", "succeeded", proxyArtifactID, []any{}),
			mediaJob("waveform", "blocked", "", []any{map[string]any{
				"kind": "executor-required", "capability": "media-executor/waveform",
			}}),
			mediaJob("transcript", "blocked", "", []any{map[string]any{
				"kind": "model-required", "resourceId": acceptanceTranscriptResource,
			}}),
		},
	}}
}

func TestInspectMediaPipelineSelectsTypedAVStreamsWithoutArrayPosition(t *testing.T) {
	projectID := "018f0000-0000-7000-8000-000000000201"
	assetID := "018f0000-0000-7000-8000-000000000202"
	audioID := "018f0000-0000-7000-8000-000000000203"
	videoID := "018f0000-0000-7000-8000-000000000204"
	factsID := "018f0000-0000-7000-8000-000000000205"
	proxyID := "018f0000-0000-7000-8000-000000000206"
	fingerprint := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	data := mediaData(projectID, assetID, audioID, factsID, proxyID, fingerprint)
	facts := record(record(data["asset"])["facts"])
	facts["container"] = "matroska,webm"
	facts["streams"] = []any{
		map[string]any{
			"id": videoID,
			"descriptor": map[string]any{
				"mediaType": "video", "codec": "vp9", "dispositions": []any{},
				"video": map[string]any{"width": json.Number("160"), "height": json.Number("90")},
			},
		},
		map[string]any{
			"id": audioID,
			"descriptor": map[string]any{
				"mediaType": "audio", "codec": "opus", "dispositions": []any{},
				"audio": map[string]any{"sampleRate": json.Number("48000"), "channels": json.Number("1")},
			},
		},
	}
	for _, value := range list(record(data["asset"])["jobs"]) {
		job := record(value)
		job["progressBasisPoints"] = json.Number(fmt.Sprint(job["progressBasisPoints"]))
	}
	observed, ready, err := inspectMediaPipeline(data, Observation{
		ProjectID: projectID, AssetID: assetID, AssetState: "online",
	}, "1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !ready || observed.MediaStreamID != audioID || observed.VideoStreamID != videoID ||
		observed.MediaContainer != "matroska,webm" || observed.MediaChannels != "1" {
		t.Fatalf("A/V observation=%+v ready=%t", observed, ready)
	}
}

func mediaJob(kind, state, artifactID string, prerequisites []any) map[string]any {
	progress := 0
	if state == "succeeded" {
		progress = 10000
	}
	job := map[string]any{
		"kind": kind, "state": state, "progressBasisPoints": progress,
		"prerequisites": prerequisites,
	}
	if artifactID != "" {
		job["resultArtifactId"] = artifactID
	}
	return job
}

func runData(projectID, runID, turnID string) map[string]any {
	return map[string]any{"run": map[string]any{
		"id": runID, "projectId": projectID, "status": "active",
		"currentTurn": map[string]any{
			"id": turnID, "status": "active", "generation": "1",
		},
	}}
}

func executableHelp(fingerprint string) map[string]any {
	return help(nil, map[string]any{
		"fingerprint": fingerprint, "input": map[string]any{}, "result": map[string]any{},
	})
}

func acceptanceEditContext(projectID, sequenceID, runID, turnID string) []string {
	return []string{
		"--project-id", projectID, "--sequence-id", sequenceID,
		"--run-id", runID, "--turn-id", turnID,
	}
}

func authoredTextData(id, text string) map[string]any {
	return map[string]any{
		"id": id, "revision": "1", "purpose": "spoken", "language": "en",
		"text": text, "tombstoned": false,
	}
}

func narrativeData(documentID, rootID, revision string, nodes []any) map[string]any {
	if nodes == nil {
		nodes = []any{}
	}
	return map[string]any{
		"documentId": documentID, "documentRevision": revision,
		"parent": map[string]any{"id": rootID, "revision": revision}, "nodes": nodes,
	}
}

func proposalData(id, digest, allocationID, status, transactionID string) map[string]any {
	proposal := map[string]any{
		"id": id, "digest": digest, "status": status,
		"allocation": []any{map[string]any{
			"local": acceptanceAuthoredTextLocal, "kind": "narrative-node", "id": allocationID,
		}},
	}
	if transactionID != "" {
		proposal["appliedTransactionId"] = transactionID
	}
	return map[string]any{"proposal": proposal}
}

func TestActorEnvironmentRejectsHarnessAndAuthorityInputs(t *testing.T) {
	actual := ActorEnvironment([]string{
		"PATH=/installed", "HOME=/creator", "OPEN_CUT_HARNESS_CDP_PORT=43123",
		"OPEN_CUT_ACCEPTANCE_CDP_ENDPOINT=http://127.0.0.1:43123", "OC_SIDECAR_TOKEN=secret",
	})
	if !reflect.DeepEqual(actual, []string{"PATH=/installed", "HOME=/creator"}) {
		t.Fatalf("actor environment=%v", actual)
	}
}

func help(children []any, extra map[string]any) map[string]any {
	value := map[string]any{"schema": helpSchema}
	if children != nil {
		value["children"] = children
	}
	for key, item := range extra {
		value[key] = item
	}
	return value
}

func child(name string, leaf bool) map[string]any {
	return map[string]any{"name": name, "leaf": leaf}
}

func result(status string, data any, commandError any) map[string]any {
	value := map[string]any{"schema": commandSchema, "status": status}
	if data != nil {
		value["data"] = data
	}
	if commandError != nil {
		value["error"] = commandError
	}
	return value
}
