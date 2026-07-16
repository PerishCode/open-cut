package productcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/authwire"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func requestCLIChallenge(
	ctx context.Context,
	endpoint string,
	invocation businessInvocation,
) (authwire.CLIChallengeResult, error) {
	clientInstance, err := domain.GenerateUUIDv7(time.Now().UTC())
	if err != nil {
		return authwire.CLIChallengeResult{}, err
	}
	request := authwire.CLIChallengeRequest{
		ClientInstance: "cli-" + clientInstance, Command: invocation.name,
		CommandFingerprint: invocation.fingerprint, Method: invocation.method,
		Path: invocation.path, Query: invocation.query, BodyDigest: invocation.bodyDigest,
		RequestID: invocation.requestID, Context: invocation.context, PolicyOverride: invocation.policyOverride,
	}
	var result authwire.CLIChallengeResult
	if err := postJSON(ctx, endpoint+authwire.CLIChallengeRoute, request, &result); err != nil {
		return authwire.CLIChallengeResult{}, err
	}
	settings := application.InvocationPolicySettings{
		Revision: result.Policy.SettingsRevision, Policy: result.Policy.Persisted,
	}
	expectedPolicy, policyErr := application.NewInvocationPolicySnapshot(settings, invocation.policyOverride)
	expectedInputDigest, digestErr := invocationDigest(invocation)
	if result.Schema != authwire.CLIChallengeSchema || result.InvocationID.IsZero() || result.Command != invocation.name ||
		result.CommandFingerprint != invocation.fingerprint || result.Path != invocation.path ||
		result.Query != invocation.query || result.BodyDigest != invocation.bodyDigest || result.Method != invocation.method ||
		result.InputDigest != expectedInputDigest || digestErr != nil || result.RequestID != invocation.requestID ||
		result.Receipt != invocation.receipt || result.Role != authwire.CLIRole ||
		policyErr != nil || result.Policy != expectedPolicy {
		return authwire.CLIChallengeResult{}, fmt.Errorf("API returned a mismatched CLI challenge")
	}
	if result.GrantID != "" {
		if _, err := domain.ParseActivityEventID(result.GrantID); err != nil {
			return authwire.CLIChallengeResult{}, fmt.Errorf("API returned an invalid CLI grant identity")
		}
		if result.GrantRevision == nil || result.GrantRevision.Value() < 1 {
			return authwire.CLIChallengeResult{}, fmt.Errorf("API returned an invalid CLI grant revision")
		}
		if _, err := domain.ParseDigest(result.GrantScopeDigest); err != nil {
			return authwire.CLIChallengeResult{}, fmt.Errorf("API returned an invalid CLI scope digest")
		}
	} else if result.GrantRevision != nil || result.GrantScopeDigest != "" {
		return authwire.CLIChallengeResult{}, fmt.Errorf("API returned unbound CLI grant authority")
	}
	return result, nil
}

func invocationDigest(invocation businessInvocation) (domain.Digest, error) {
	canonical, err := json.Marshal(struct {
		BodyDigest         string          `json:"bodyDigest"`
		Command            string          `json:"command"`
		CommandFingerprint string          `json:"commandFingerprint"`
		Context            command.Context `json:"context"`
		Method             string          `json:"method"`
		Path               string          `json:"path"`
		Query              string          `json:"query"`
		RequestID          string          `json:"requestId,omitempty"`
	}{
		BodyDigest: invocation.bodyDigest, Command: invocation.name, CommandFingerprint: invocation.fingerprint,
		Context: invocation.context, Method: invocation.method, Path: invocation.path,
		Query: invocation.query, RequestID: invocation.requestID,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return domain.ParseDigest("sha256:" + hex.EncodeToString(digest[:]))
}

func postJSON(ctx context.Context, target string, input, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return fmt.Errorf("CLI challenge returned %d: %s", response.StatusCode, string(message))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 256<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
}
