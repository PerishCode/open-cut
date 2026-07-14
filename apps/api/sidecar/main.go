package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/PerishCode/open-cut/apps/api/controller"
	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/apps/api/service"
	sidecarclient "github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

const httpEndpoint = "http"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "openapi" {
		_, api := controller.NewRouter(
			service.NewHealth(repository.StaticHealth{}),
			service.NewProjects(repository.NewMemoryProjects()),
		)
		document, err := api.OpenAPI().MarshalJSON()
		if err != nil {
			return fmt.Errorf("encode OpenAPI: %w", err)
		}
		_, err = os.Stdout.Write(append(document, '\n'))
		return err
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: api-sidecar [openapi]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	launch, err := protocol.LoadLaunchEnvironment()
	if err != nil {
		return err
	}
	dataDir, err := sidecarclient.ResolveDataDir(launch)
	if err != nil {
		return fmt.Errorf("resolve API data directory: %w", err)
	}
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		return err
	}
	defer projects.Close()
	mux, _ := controller.NewRouter(
		service.NewHealth(repository.StaticHealth{}),
		service.NewProjects(projects),
	)
	session, err := sidecarclient.DialSession(ctx, launch.Control, launch.Token, sidecarclient.Registration{
		Channel: launch.Channel, Namespace: launch.Namespace, App: launch.App,
		Mode: launch.Mode, Source: launch.Source,
	})
	if err != nil {
		return fmt.Errorf("connect API sidecar: %w", err)
	}
	defer session.Close(0)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for API: %w", err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	endpoint := "http://" + listener.Addr().String()
	if err := session.Endpoint(httpEndpoint, endpoint); err != nil {
		return shutdownServer(server, fmt.Errorf("publish API endpoint: %w", err))
	}
	if err := session.Ready(); err != nil {
		return shutdownServer(server, fmt.Errorf("publish API readiness: %w", err))
	}

	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return shutdownServer(server, nil)
		case err := <-served:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("serve API: %w", err)
		case <-heartbeat.C:
			_ = session.Heartbeat()
		default:
			commandContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			command, readErr := session.ReadCommand(commandContext)
			cancel()
			if readErr == nil && command == protocol.ControlCommandShutdown {
				return shutdownServer(server, nil)
			}
		}
	}
}

func shutdownServer(server *http.Server, result error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); result == nil && err != nil {
		return err
	}
	return result
}
