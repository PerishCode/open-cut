package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/PerishCode/open-cut/internal/productcli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	executable, _ := os.Executable()
	os.Exit(productcli.Run(ctx, os.Args[1:], productcli.Options{
		Executable: executable,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}))
}
