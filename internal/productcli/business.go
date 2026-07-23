package productcli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

const maximumAPIResponseBytes = 4 << 20

type businessInvocation struct {
	name           string
	method         string
	path           string
	query          string
	body           []byte
	bodyDigest     string
	context        command.Context
	policyOverride application.InvocationPolicyOverride
	policy         application.InvocationPolicy
	fingerprint    string
	receipt        command.ReceiptPolicy
	requestID      string
}

func runBusiness(
	ctx context.Context,
	bootstrapPath, cliVersion string,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	invocation, err := parseBusinessInvocation(args, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	endpoint, err := resolveAPIEndpoint(ctx, bootstrapPath)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusUnavailable, "product-unavailable", err, "")
	}
	return runBusinessInvocation(ctx, endpoint, cliVersion, invocation, stdout, stderr)
}

func runBusinessInvocation(
	ctx context.Context,
	endpoint, cliVersion string,
	invocation businessInvocation,
	stdout, stderr io.Writer,
) int {
	challenge, err := requestCLIChallenge(ctx, endpoint, invocation)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusUnavailable, "challenge-unavailable", err, "")
	}
	invocation.policy = challenge.Policy.Effective
	payload, err := base64.RawURLEncoding.DecodeString(challenge.SigningPayload)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-challenge", err, "")
	}
	signed, err := lifecycle.RequestSignature(ctx, authwire.CLIRole, payload)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusUnavailable, "signer-unavailable", err, "")
	}
	if signed.InstallationID != challenge.InstallationID ||
		signed.InstallationGeneration != challenge.InstallationGeneration || signed.Role != challenge.Role {
		return writeCommandFailure(
			stdout, stderr, cliVersion, invocation, command.StatusFailed,
			"signer-identity-mismatch", fmt.Errorf("signer identity did not match API challenge"), "",
		)
	}
	response, err := requestBusiness(ctx, endpoint, invocation, challenge, signed.Signature)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusUnavailable, "product-unavailable", err, "")
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maximumAPIResponseBytes+1))
	if readErr != nil || len(data) > maximumAPIResponseBytes {
		if readErr == nil {
			readErr = fmt.Errorf("API response exceeded the command limit")
		}
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-product-response", readErr, "")
	}
	if response.StatusCode != http.StatusOK {
		return writeHTTPFailure(stdout, stderr, cliVersion, invocation, response, data)
	}
	raw, revision, cursor, err := validateBusinessResponse(invocation.name, data)
	if err != nil {
		return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-product-response", err, "")
	}
	resultStatus := command.StatusSucceeded
	if invocation.name == "asset frames" {
		var frames command.AssetFramesData
		if err := json.Unmarshal(raw, &frames); err != nil {
			return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-product-response", err, "")
		}
		if frames.Status == application.MediaFrameSetAccepted {
			resultStatus = command.StatusAccepted
		}
	} else if invocation.name == "sequence frames" {
		var frames command.SequenceFramesData
		if err := json.Unmarshal(raw, &frames); err != nil {
			return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-product-response", err, "")
		}
		switch frames.Status {
		case application.SequenceFrameSetAccepted:
			resultStatus = command.StatusAccepted
		case application.SequenceFrameSetFailed:
			resultStatus = command.StatusFailed
		}
	} else if strings.HasPrefix(invocation.name, "export ") {
		resultStatus, err = exportResultStatus(invocation.name, raw)
		if err != nil {
			return writeCommandFailure(stdout, stderr, cliVersion, invocation, command.StatusFailed, "invalid-product-response", err, "")
		}
	}
	result := command.Result[json.RawMessage]{
		Schema: command.CommandSchemaVersion, CLIVersion: cliVersion, Command: invocation.name,
		Context: invocation.context, Status: resultStatus,
		Data: &raw, ProjectRevision: revision, ActivityCursor: cursor,
	}
	if err := encodeCommandResult(stdout, result, invocation.policy); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseBusinessInvocation(args []string, stdin io.Reader, stderr io.Writer) (businessInvocation, error) {
	if len(args) < 2 {
		return businessInvocation{}, fmt.Errorf("business command requires <command> <subcommand>")
	}
	name := args[0] + " " + args[1]
	registry := command.InitialRegistry()
	descriptor, err := registry.Lookup(args[:2])
	if err != nil {
		return businessInvocation{}, command.ErrUnknownCommand
	}
	fingerprint, err := registry.Fingerprint(args[:2])
	if err != nil {
		return businessInvocation{}, command.ErrUnknownCommand
	}
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	stateFlags := addAppStateFlags(set)
	values := url.Values{}
	var bodyValue any
	var documentID domain.NarrativeDocumentID
	var parentID domain.NarrativeNodeID
	var proposalID domain.ProposalID
	var transactionID domain.TransactionID
	var entityKind domain.EditEntityKind
	var entityID string
	var assetID domain.AssetID
	var exportJobID domain.WorkJobID
	switch name {
	case "product status":
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid product status invocation")
		}
	case "project list":
		status := set.String("status", "", "lifecycle status")
		after := set.String("after", "", "query-local continuation cursor")
		limit := set.Uint("limit", 0, "maximum Project summaries")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 100 {
			return businessInvocation{}, fmt.Errorf("invalid project list invocation")
		}
		if *status != "" {
			if *status != "active" && *status != "archived" && *status != "tombstoned" {
				return businessInvocation{}, fmt.Errorf("invalid project status")
			}
			values.Set("status", *status)
		}
		if *after != "" {
			values.Set("after", *after)
		}
		if *limit != 0 {
			values.Set("limit", strconv.FormatUint(uint64(*limit), 10))
		}
	case "project show":
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid project show invocation")
		}
	case "asset list":
		after := set.String("after", "", "query-local continuation cursor")
		limit := set.Uint("limit", 0, "maximum Asset summaries")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 100 {
			return businessInvocation{}, fmt.Errorf("invalid asset list invocation")
		}
		setBoundedQuery(values, "after", *after, "limit", *limit)
	case "asset inspect":
		asset := set.String("asset-id", "", "Asset identity")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid asset inspect invocation")
		}
		assetID, err = domain.ParseAssetID(*asset)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid Asset identity")
		}
	case "asset frames":
		parsedAsset, _, frameInput, parseErr := parseAssetFramesInvocation(set, args[2:])
		if parseErr != nil {
			return businessInvocation{}, parseErr
		}
		assetID, bodyValue = parsedAsset, frameInput
	case "sequence frames":
		frameInput, parseErr := parseSequenceFramesInvocation(set, args[2:])
		if parseErr != nil {
			return businessInvocation{}, parseErr
		}
		bodyValue = frameInput
	case "export start", "export show", "export retry", "export cancel":
		bodyValue, exportJobID, err = parseExportInvocation(name, set, args[2:])
		if err != nil {
			return businessInvocation{}, err
		}
	case "transcript read":
		asset := set.String("asset-id", "", "Asset identity")
		artifact := set.String("artifact-id", "", "optional exact TranscriptArtifact identity")
		after := set.String("after", "", "last segment ordinal returned by the previous page")
		limit := set.Uint("limit", 0, "maximum transcript segments")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 50 {
			return businessInvocation{}, fmt.Errorf("invalid transcript read invocation")
		}
		assetID, err = domain.ParseAssetID(*asset)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid Asset identity")
		}
		if *artifact != "" {
			parsed, parseErr := domain.ParseArtifactID(*artifact)
			if parseErr != nil {
				return businessInvocation{}, fmt.Errorf("invalid TranscriptArtifact identity")
			}
			values.Set("artifactId", parsed.String())
		}
		if *after != "" {
			ordinal, parseErr := strconv.ParseUint(*after, 10, 32)
			if parseErr != nil || strconv.FormatUint(ordinal, 10) != *after {
				return businessInvocation{}, fmt.Errorf("invalid transcript continuation")
			}
			values.Set("after", *after)
		}
		if *limit != 0 {
			values.Set("limit", strconv.FormatUint(uint64(*limit), 10))
		}
	case "activity list":
		after := set.String("after", "", "activity cursor")
		limit := set.Uint("limit", 0, "maximum activity events")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 500 {
			return businessInvocation{}, fmt.Errorf("invalid activity list invocation")
		}
		if *after != "" {
			var cursor domain.Cursor
			if err := cursor.UnmarshalText([]byte(*after)); err != nil {
				return businessInvocation{}, fmt.Errorf("invalid activity cursor")
			}
			values.Set("after", *after)
		}
		if *limit != 0 {
			values.Set("limit", strconv.FormatUint(uint64(*limit), 10))
		}
	case "run begin":
		requestID := set.String("request-id", "", "idempotent request identity")
		intent := set.String("intent", "", "durable AgentRun intent")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *intent == "" {
			return businessInvocation{}, fmt.Errorf("invalid run begin invocation")
		}
		parsedRequest, err := domain.ParseRequestID(*requestID)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid run request identity")
		}
		bodyValue = command.RunBeginInput{RequestID: parsedRequest, Intent: *intent}
	case "run show":
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid run show invocation")
		}
	case "run wait":
		after := set.String("after", "", "last observed AgentRun activity cursor")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid run wait invocation")
		}
		if *after != "" {
			var cursor domain.Cursor
			if err := cursor.UnmarshalText([]byte(*after)); err != nil {
				return businessInvocation{}, fmt.Errorf("invalid AgentRun activity cursor")
			}
			values.Set("after", *after)
		}
	case "run resume", "run complete", "run cancel":
		requestID := set.String("request-id", "", "idempotent request identity")
		expected := set.String("expected-generation", "", "expected current AgentTurn generation")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid %s invocation", name)
		}
		parsedRequest, err := domain.ParseRequestID(*requestID)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid run request identity")
		}
		var generation domain.Revision
		if err := generation.UnmarshalText([]byte(*expected)); err != nil || generation.Value() < 1 {
			return businessInvocation{}, fmt.Errorf("invalid expected AgentTurn generation")
		}
		bodyValue = command.RunResumeInput{RequestID: parsedRequest, ExpectedGeneration: generation}
	case "narrative show":
		document := set.String("document-id", "", "Narrative document identity")
		parent := set.String("parent-id", "", "Narrative section identity")
		after := set.String("after", "", "query-local continuation cursor")
		limit := set.Uint("limit", 0, "maximum PaperEdit child nodes")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 200 {
			return businessInvocation{}, fmt.Errorf("invalid narrative show invocation")
		}
		documentID, err = domain.ParseNarrativeDocumentID(*document)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid Narrative document identity")
		}
		parentID, err = domain.ParseNarrativeNodeID(*parent)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid Narrative parent identity")
		}
		values.Set("parentId", parentID.String())
		setBoundedQuery(values, "after", *after, "limit", *limit)
	case "sequence show":
		track := set.String("track-id", "", "optional track identity")
		start := set.String("start", "", "exact window start as value/scale seconds")
		duration := set.String("duration", "", "exact positive window duration as value/scale seconds")
		after := set.String("after", "", "query-local continuation cursor")
		limit := set.Uint("limit", 0, "maximum captions")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 512 {
			return businessInvocation{}, fmt.Errorf("invalid sequence show invocation")
		}
		startValue, parseErr := parseRationalArgument(*start, false)
		if parseErr != nil {
			return businessInvocation{}, fmt.Errorf("invalid Sequence start")
		}
		durationValue, parseErr := parseRationalArgument(*duration, true)
		if parseErr != nil {
			return businessInvocation{}, fmt.Errorf("invalid Sequence duration")
		}
		values.Set("startValue", startValue.Value.String())
		values.Set("startScale", strconv.FormatInt(int64(startValue.Scale), 10))
		values.Set("durationValue", durationValue.Value.String())
		values.Set("durationScale", strconv.FormatInt(int64(durationValue.Scale), 10))
		if *track != "" {
			parsed, parseErr := domain.ParseTrackID(*track)
			if parseErr != nil {
				return businessInvocation{}, fmt.Errorf("invalid track identity")
			}
			values.Set("trackId", parsed.String())
		}
		setBoundedQuery(values, "after", *after, "limit", *limit)
	case "entity show":
		kind := set.String("kind", "", "editable entity kind")
		id := set.String("id", "", "editable entity identity")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid entity show invocation")
		}
		entityKind = domain.EditEntityKind(*kind)
		if !validCLIEditEntityID(entityKind, *id) {
			return businessInvocation{}, fmt.Errorf("invalid editable entity")
		}
		entityID = *id
	case "edit show":
		proposal := set.String("proposal-id", "", "Edit Proposal identity")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid edit show invocation")
		}
		proposalID, err = domain.ParseProposalID(*proposal)
		if err != nil {
			return businessInvocation{}, fmt.Errorf("invalid Edit Proposal identity")
		}
	case "edit history":
		after := set.String("after", "", "committed Project revision")
		limit := set.Uint("limit", 0, "maximum transactions")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *limit > 100 {
			return businessInvocation{}, fmt.Errorf("invalid edit history invocation")
		}
		if *after != "" {
			var revision domain.Revision
			if revision.UnmarshalText([]byte(*after)) != nil {
				return businessInvocation{}, fmt.Errorf("invalid Edit history revision")
			}
			values.Set("after", revision.String())
		}
		if *limit != 0 {
			values.Set("limit", strconv.FormatUint(uint64(*limit), 10))
		}
	case "edit derive-captions":
		sourceExcerpt := set.String("source-excerpt-id", "", "exact SourceExcerpt identity")
		clip := set.String("clip-id", "", "exact target Clip identity")
		track := set.String("track-id", "", "target Caption Track identity")
		localPrefix := set.String("local-prefix", "derived", "proposal-local output prefix")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid edit derive-captions invocation")
		}
		sourceExcerptID, parseErr := domain.ParseNarrativeNodeID(*sourceExcerpt)
		clipID, clipErr := domain.ParseClipID(*clip)
		trackID, trackErr := domain.ParseTrackID(*track)
		prefixID, prefixErr := domain.ParseLocalID(*localPrefix)
		if parseErr != nil || clipErr != nil || trackErr != nil || prefixErr != nil || len(*localPrefix) > 40 {
			return businessInvocation{}, fmt.Errorf("invalid caption derivation identity")
		}
		values.Set("sourceExcerptId", sourceExcerptID.String())
		values.Set("clipId", clipID.String())
		values.Set("trackId", trackID.String())
		values.Set("localPrefix", prefixID.String())
	case "edit derive-rough-cut":
		input := set.String("input", "", "strict rough-cut planning JSON; use - for stdin")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *input != "-" {
			return businessInvocation{}, fmt.Errorf("edit derive-rough-cut requires --input -")
		}
		decoded, decodeErr := readRoughCutDerivationInput(stdin)
		if decodeErr != nil {
			return businessInvocation{}, decodeErr
		}
		bodyValue = decoded
	case "edit propose":
		input := set.String("input", "", "strict proposal JSON input; use - for stdin")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 || *input != "-" {
			return businessInvocation{}, fmt.Errorf("edit propose requires --input -")
		}
		decoded, decodeErr := readEditProposalInput(stdin)
		if decodeErr != nil {
			return businessInvocation{}, decodeErr
		}
		bodyValue = decoded
	case "edit apply":
		proposal := set.String("proposal-id", "", "Edit Proposal identity")
		requestID := set.String("request-id", "", "idempotent request identity")
		digest := set.String("proposal-digest", "", "exact Proposal digest")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid edit apply invocation")
		}
		proposalID, err = domain.ParseProposalID(*proposal)
		request, requestErr := domain.ParseRequestID(*requestID)
		proposalDigest, digestErr := domain.ParseDigest(*digest)
		if err != nil || requestErr != nil || digestErr != nil {
			return businessInvocation{}, fmt.Errorf("invalid edit apply identity")
		}
		bodyValue = command.EditApplyInput{RequestID: request, ProposalDigest: proposalDigest}
	case "edit undo":
		transaction := set.String("transaction-id", "", "Edit Transaction identity")
		requestID := set.String("request-id", "", "idempotent request identity")
		intent := set.String("intent", "", "optional undo intent")
		if err := set.Parse(args[2:]); err != nil || set.NArg() != 0 {
			return businessInvocation{}, fmt.Errorf("invalid edit undo invocation")
		}
		transactionID, err = domain.ParseTransactionID(*transaction)
		request, requestErr := domain.ParseRequestID(*requestID)
		if err != nil || requestErr != nil || len(*intent) > application.MaximumEditIntentBytes {
			return businessInvocation{}, fmt.Errorf("invalid edit undo identity")
		}
		bodyValue = command.EditUndoInput{RequestID: request, Intent: *intent}
	default:
		return businessInvocation{}, command.ErrUnknownCommand
	}
	appState, err := stateFlags.Resolve()
	if err != nil {
		return businessInvocation{}, err
	}
	path := ""
	switch name {
	case "product status":
		path = "/v1/product/status"
	case "project list":
		path = "/v1/projects"
	case "project show":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("project show requires --project-id or OPEN_CUT_PROJECT_ID")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String()
	case "asset list":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("asset list requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/assets"
	case "asset inspect":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("asset inspect requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/assets/" + assetID.String()
	case "asset frames":
		if appState.context.ProjectID == nil || appState.context.RunID == nil || appState.context.TurnID == nil {
			return businessInvocation{}, fmt.Errorf("asset frames requires project, run, and turn context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/runs/" +
			appState.context.RunID.String() + "/turns/" + appState.context.TurnID.String() +
			"/assets/" + assetID.String() + "/frames"
	case "sequence frames":
		if appState.context.ProjectID == nil || appState.context.SequenceID == nil ||
			appState.context.RunID == nil || appState.context.TurnID == nil {
			return businessInvocation{}, fmt.Errorf("sequence frames requires project, sequence, run, and turn context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/runs/" +
			appState.context.RunID.String() + "/turns/" + appState.context.TurnID.String() +
			"/sequences/" + appState.context.SequenceID.String() + "/frames"
	case "export start", "export show", "export retry", "export cancel":
		path, err = exportInvocationPath(name, appState.context, exportJobID)
	case "transcript read":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("transcript read requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/assets/" +
			assetID.String() + "/transcript"
	case "activity list":
		path = "/v1/activity"
		if appState.context.ProjectID != nil {
			values.Set("projectId", appState.context.ProjectID.String())
		}
	case "run begin":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("run begin requires --project-id or OPEN_CUT_PROJECT_ID")
		}
		if appState.context.RunID != nil || appState.context.TurnID != nil {
			return businessInvocation{}, fmt.Errorf("run begin is unavailable inside an AgentBridge turn")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/runs"
	case "run show", "run wait":
		if appState.context.ProjectID == nil || appState.context.RunID == nil {
			return businessInvocation{}, fmt.Errorf("%s requires project and run context", name)
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/runs/" + appState.context.RunID.String()
		if name == "run wait" {
			path += "/wait"
		}
	case "run resume", "run complete", "run cancel":
		if appState.context.ProjectID == nil || appState.context.RunID == nil || appState.context.TurnID == nil {
			return businessInvocation{}, fmt.Errorf("%s requires project, run, and turn context", name)
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/runs/" + appState.context.RunID.String() +
			"/turns/" + appState.context.TurnID.String() + "/" + args[1]
	case "narrative show":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("narrative show requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/narratives/" + documentID.String() + "/subtree"
	case "sequence show":
		if appState.context.ProjectID == nil || appState.context.SequenceID == nil {
			return businessInvocation{}, fmt.Errorf("sequence show requires project and sequence context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/sequences/" + appState.context.SequenceID.String() + "/window"
	case "entity show":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("entity show requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/entities/" + string(entityKind) + "/" + entityID
	case "edit show":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("edit show requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/edit/proposals/" + proposalID.String()
	case "edit history":
		if appState.context.ProjectID == nil {
			return businessInvocation{}, fmt.Errorf("edit history requires project context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/edit/transactions"
	case "edit derive-captions":
		if appState.context.ProjectID == nil || appState.context.SequenceID == nil {
			return businessInvocation{}, fmt.Errorf("edit derive-captions requires project and sequence context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/sequences/" +
			appState.context.SequenceID.String() + "/edit/caption-derivation"
	case "edit derive-rough-cut":
		if appState.context.ProjectID == nil || appState.context.SequenceID == nil {
			return businessInvocation{}, fmt.Errorf("edit derive-rough-cut requires project and sequence context")
		}
		path = "/v1/projects/" + appState.context.ProjectID.String() + "/sequences/" +
			appState.context.SequenceID.String() + "/edit/rough-cut-derivation"
	case "edit propose":
		path, err = editCommandPath(appState.context, "proposals")
	case "edit apply":
		path, err = editCommandPath(appState.context, "proposals/"+proposalID.String()+"/apply")
	case "edit undo":
		path, err = editCommandPath(appState.context, "transactions/"+transactionID.String()+"/undo")
	}
	if err != nil {
		return businessInvocation{}, err
	}
	method := http.MethodGet
	var body []byte
	bodyDigest := authwire.NoBodyDigest(name)
	if bodyValue != nil {
		method = http.MethodPost
		body, err = json.Marshal(bodyValue)
		if err != nil {
			return businessInvocation{}, err
		}
		digest, err := authwire.CommandBodyDigest(name, body)
		if err != nil {
			return businessInvocation{}, err
		}
		bodyDigest = digest.String()
	}
	requestID := ""
	if descriptor.RequestIdentity {
		var identity struct {
			RequestID domain.RequestID `json:"requestId"`
		}
		if len(body) == 0 || json.Unmarshal(body, &identity) != nil {
			return businessInvocation{}, fmt.Errorf("command request identity is missing")
		}
		if _, err := domain.ParseRequestID(identity.RequestID.String()); err != nil {
			return businessInvocation{}, fmt.Errorf("command request identity is invalid")
		}
		requestID = identity.RequestID.String()
	}
	return businessInvocation{
		name: name, method: method, path: path, query: values.Encode(), body: body, bodyDigest: bodyDigest,
		context:        appState.context,
		policyOverride: appState.policyOverride, policy: application.DefaultInvocationPolicy(), fingerprint: fingerprint,
		receipt: descriptor.Receipt, requestID: requestID,
	}, nil
}

func validateLoopbackEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Port() == "" ||
		(parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("product API endpoint is not a trusted loopback URL")
	}
	return nil
}

func encodeCommandResult(writer io.Writer, value any, policy application.InvocationPolicy) error {
	encoder := json.NewEncoder(writer)
	if policy.Output == application.OutputHuman {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func requestBusiness(
	ctx context.Context,
	endpoint string,
	invocation businessInvocation,
	challenge authwire.CLIChallengeResult,
	signature string,
) (*http.Response, error) {
	target := endpoint + invocation.path
	if invocation.query != "" {
		target += "?" + invocation.query
	}
	request, err := http.NewRequestWithContext(ctx, invocation.method, target, bytes.NewReader(invocation.body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if len(invocation.body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(authwire.HeaderChallenge, challenge.Nonce)
	request.Header.Set(authwire.HeaderSignature, signature)
	if challenge.GrantID != "" {
		request.Header.Set(authwire.HeaderGrant, challenge.GrantID)
	}
	return (&http.Client{Timeout: 30 * time.Second}).Do(request)
}

func writeHTTPFailure(
	stdout, stderr io.Writer,
	cliVersion string,
	invocation businessInvocation,
	response *http.Response,
	body []byte,
) int {
	authStatus := response.Header.Get(authwire.HeaderAuthStatus)
	pairingID := response.Header.Get(authwire.HeaderPairingID)
	if authStatus == authwire.AuthStatusPairingRequired {
		return writeCommandFailure(
			stdout, stderr, cliVersion, invocation, command.StatusPairingRequired,
			"pairing-required", nil, pairingID,
		)
	}
	if authStatus == authwire.AuthStatusScopeUpgradeRequired {
		return writeCommandFailure(
			stdout, stderr, cliVersion, invocation, command.StatusScopeUpgradeRequired,
			"scope-upgrade-required", nil, response.Header.Get(authwire.HeaderScopeUpgradeID),
		)
	}
	status := command.StatusFailed
	code := "product-request-failed"
	if response.StatusCode == http.StatusNotFound {
		status = command.StatusNotFound
		code = "not-found"
	} else if response.StatusCode == http.StatusUnprocessableEntity {
		status = command.StatusInvalid
		code = "invalid"
	} else if response.StatusCode == http.StatusConflict {
		status = command.StatusConflict
		code = "conflict"
		if response.Header.Get(command.StatusHeader) == string(command.StatusStaleTurn) {
			status = command.StatusStaleTurn
			code = "stale-turn"
		}
	} else if authStatus != "" {
		code = authStatus
	} else if response.StatusCode >= 500 {
		status = command.StatusUnavailable
		code = "product-unavailable"
	}
	return writeCommandFailureMessage(
		stdout, stderr, cliVersion, invocation, status, code,
		fmt.Errorf("product API returned %d: %s", response.StatusCode, strings.TrimSpace(string(body))), "",
		productErrorReason(body),
	)
}

// productErrorReason lifts the human-readable reason out of a product API
// error body so a structured Agent result can carry it, instead of leaving the
// explanation only in the raw stderr line. It returns "" when the body has no
// more detail than the generic title.
func productErrorReason(body []byte) string {
	var document struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &document) != nil {
		return ""
	}
	reasons := make([]string, 0, len(document.Errors))
	for _, item := range document.Errors {
		trimmed := strings.TrimSpace(item.Message)
		if trimmed != "" && trimmed != document.Detail {
			reasons = append(reasons, trimmed)
		}
	}
	message := strings.TrimSpace(document.Detail)
	if len(reasons) > 0 {
		joined := strings.Join(reasons, "; ")
		if message == "" {
			message = joined
		} else {
			message += ": " + joined
		}
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

func writeCommandFailure(
	stdout, stderr io.Writer,
	cliVersion string,
	invocation businessInvocation,
	status command.Status,
	code string,
	cause error,
	entityID string,
) int {
	return writeCommandFailureMessage(stdout, stderr, cliVersion, invocation, status, code, cause, entityID, "")
}

func writeCommandFailureMessage(
	stdout, stderr io.Writer,
	cliVersion string,
	invocation businessInvocation,
	status command.Status,
	code string,
	cause error,
	entityID string,
	message string,
) int {
	if cause != nil {
		fmt.Fprintln(stderr, cause)
	}
	issue := command.Issue{Code: code, EntityID: entityID, Message: message}
	result := command.Result[json.RawMessage]{
		Schema: command.CommandSchemaVersion, CLIVersion: cliVersion, Command: invocation.name,
		Context: invocation.context, Status: status,
		Error: &command.Error{Code: code, Issues: []command.Issue{issue}},
	}
	if err := encodeCommandResult(stdout, result, invocation.policy); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
