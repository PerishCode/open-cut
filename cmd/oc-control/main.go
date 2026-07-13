package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/PerishCode/open-cut/internal/controlcli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(controlcli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
