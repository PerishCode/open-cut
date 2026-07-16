package lifecycle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func RequestSignature(ctx context.Context, role string, payload []byte) (SignerResponse, error) {
	if role == "" || len(payload) == 0 || len(payload) > maximumSigningBytes {
		return SignerResponse{}, fmt.Errorf("invalid lifecycle signing request")
	}
	request := SignerRequest{
		Schema: SignerRequestSchema, Role: role,
		Payload: base64.RawURLEncoding.EncodeToString(payload),
	}
	var response SignerResponse
	var err error
	if socket := os.Getenv(SignerSocketEnvironment); socket != "" {
		response, err = requestSocketSignature(ctx, socket, request)
	} else if host := os.Getenv(PlatformHostEnvironment); host != "" {
		response, err = requestPlatformSignature(ctx, host, request)
	} else {
		return SignerResponse{}, fmt.Errorf("lifecycle signer is unavailable")
	}
	if err != nil {
		return SignerResponse{}, err
	}
	if response.Schema != SignerRequestSchema || response.Role != role || response.InstallationID == "" ||
		response.InstallationGeneration < 1 {
		return SignerResponse{}, fmt.Errorf("lifecycle signer returned an invalid identity")
	}
	signature, err := base64.RawURLEncoding.DecodeString(response.Signature)
	if err != nil || len(signature) == 0 {
		return SignerResponse{}, fmt.Errorf("lifecycle signer returned an invalid signature")
	}
	return response, nil
}

func requestSocketSignature(
	ctx context.Context,
	socket string,
	request SignerRequest,
) (SignerResponse, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialDevelopmentSigner(ctx, socket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return postSignerRequest(ctx, client, "http://unix"+SignerPath, request)
}

func requestPlatformSignature(
	ctx context.Context,
	host string,
	request SignerRequest,
) (SignerResponse, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return SignerResponse{}, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run(ctx, ProcessSpec{
		Executable: host, Args: []string{"__sign"}, Env: os.Environ(),
		Stdin: bytes.NewReader(encoded), Stdout: &stdout, Stderr: &stderr,
		Profile: ProfileProduction,
	}); err != nil {
		return SignerResponse{}, fmt.Errorf("platform signer failed: %w: %s", err, stderr.String())
	}
	if stdout.Len() > 256<<10 {
		return SignerResponse{}, fmt.Errorf("platform signer response is too large")
	}
	return decodeSignerResponse(&stdout)
}

func postSignerRequest(
	ctx context.Context,
	client *http.Client,
	url string,
	request SignerRequest,
) (SignerResponse, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return SignerResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return SignerResponse{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return SignerResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return SignerResponse{}, fmt.Errorf("lifecycle signer returned %d: %s", response.StatusCode, string(message))
	}
	return decodeSignerResponse(io.LimitReader(response.Body, 256<<10))
}

func decodeSignerResponse(reader io.Reader) (SignerResponse, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var response SignerResponse
	if err := decoder.Decode(&response); err != nil {
		return SignerResponse{}, err
	}
	return response, nil
}
