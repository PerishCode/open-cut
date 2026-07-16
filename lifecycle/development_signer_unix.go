//go:build !windows

package lifecycle

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func listenDevelopmentSigner(socket string) (string, net.Listener, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return "", nil, nil, err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return "", nil, nil, fmt.Errorf("development signer path exists and is not a socket")
		}
		if err := os.Remove(socket); err != nil {
			return "", nil, nil, err
		}
	} else if !os.IsNotExist(err) {
		return "", nil, nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return "", nil, nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socket)
		return "", nil, nil, err
	}
	cleanup := func() error {
		err := os.Remove(socket)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return socket, listener, cleanup, nil
}

func dialDevelopmentSigner(ctx context.Context, endpoint string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", endpoint)
}
