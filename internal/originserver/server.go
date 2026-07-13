package originserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Server struct {
	Root     string `json:"root"`
	Endpoint string `json:"endpoint"`
	listener net.Listener
	server   *http.Server
	errors   chan error
}

func Start(root, address string) (*Server, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("origin root must be an existing directory")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !tcpAddress.IP.IsLoopback() {
		listener.Close()
		return nil, fmt.Errorf("release origin may listen only on loopback TCP")
	}
	httpServer := &http.Server{
		Handler: http.FileServer(http.Dir(absolute)), ReadHeaderTimeout: 5 * time.Second,
	}
	server := &Server{
		Root: absolute, Endpoint: "http://" + listener.Addr().String(),
		listener: listener, server: httpServer, errors: make(chan error, 1),
	}
	go func() {
		err := httpServer.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		server.errors <- err
	}()
	return server, nil
}

func (server *Server) Wait() error { return <-server.errors }

func (server *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return server.server.Shutdown(ctx)
}
