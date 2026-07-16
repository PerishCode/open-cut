//go:build windows

package lifecycle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const windowsPipePrefix = `\\.\pipe\open-cut-lifecycle-signer-`

func listenDevelopmentSigner(socket string) (string, net.Listener, func() error, error) {
	if _, err := os.Lstat(socket); err == nil {
		return "", nil, nil, fmt.Errorf("development signer path exists and is not a socket")
	} else if !os.IsNotExist(err) {
		return "", nil, nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve development signer owner: %w", err)
	}
	userSID := user.User.Sid.String()
	if userSID == "" {
		return "", nil, nil, fmt.Errorf("resolve development signer owner: empty user SID")
	}
	digest := sha256.Sum256([]byte(strings.ToLower(socket)))
	endpoint := fmt.Sprintf("%s%x", windowsPipePrefix, digest[:16])
	listener, err := winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", userSID),
		InputBufferSize:    maximumSigningBytes * 2,
		OutputBufferSize:   maximumSigningBytes * 2,
	})
	if err != nil {
		return "", nil, nil, err
	}
	return endpoint, listener, nil, nil
}

func dialDevelopmentSigner(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, endpoint)
}
