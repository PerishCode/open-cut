package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/PerishCode/open-cut/internal/renderhelper"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "--execution" {
		fmt.Fprintln(os.Stderr, "usage: open-cut-render --execution <absolute execution.json>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := renderhelper.Run(ctx, os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
