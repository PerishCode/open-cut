package lifecycle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	SignerSocketEnvironment = "OC_LIFECYCLE_SIGNER_SOCKET"
	SignerRequestSchema     = 1
	SignerPath              = "/v1/sign"
	maximumSigningBytes     = 64 << 10
)

type SignerRequest struct {
	Schema  int    `json:"schema"`
	Role    string `json:"role"`
	Payload string `json:"payload"`
}

type SignerResponse struct {
	Schema                 int    `json:"schema"`
	InstallationID         string `json:"installationId"`
	InstallationGeneration uint64 `json:"installationGeneration"`
	Role                   string `json:"role"`
	Signature              string `json:"signature"`
}

type DevelopmentSigner struct {
	socket   string
	server   *http.Server
	listener net.Listener
}

func StartDevelopmentSigner(socket string, identity DevelopmentInstallationIdentity) (*DevelopmentSigner, error) {
	if socket == "" || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return nil, fmt.Errorf("development signer socket must be a clean absolute path")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("development signer path exists and is not a socket")
		}
		if err := os.Remove(socket); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		os.Remove(socket)
		return nil, err
	}
	assertion := identity.Assertion()
	handler := http.NewServeMux()
	handler.HandleFunc(SignerPath, func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maximumSigningBytes*2)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var input SignerRequest
		if err := decoder.Decode(&input); err != nil || input.Schema != SignerRequestSchema {
			http.Error(response, "invalid signing request", http.StatusUnprocessableEntity)
			return
		}
		payload, err := base64.RawURLEncoding.DecodeString(input.Payload)
		if err != nil || len(payload) == 0 || len(payload) > maximumSigningBytes {
			http.Error(response, "invalid signing payload", http.StatusUnprocessableEntity)
			return
		}
		signature, err := identity.Sign(input.Role, payload)
		if err != nil {
			http.Error(response, "signing role unavailable", http.StatusForbidden)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(SignerResponse{
			Schema: SignerRequestSchema, InstallationID: assertion.InstallationID,
			InstallationGeneration: assertion.Generation, Role: input.Role,
			Signature: base64.RawURLEncoding.EncodeToString(signature),
		})
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second}
	signer := &DevelopmentSigner{socket: socket, server: server, listener: listener}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = listener.Close()
		}
	}()
	return signer, nil
}

func (signer *DevelopmentSigner) Socket() string {
	return signer.socket
}

func (signer *DevelopmentSigner) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownErr := signer.server.Shutdown(ctx)
	listenerErr := signer.listener.Close()
	removeErr := os.Remove(signer.socket)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	return errors.Join(shutdownErr, listenerErr, removeErr)
}
