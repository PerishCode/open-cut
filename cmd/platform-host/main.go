package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/PerishCode/open-cut/internal/install"
)

func main() {
	receipt := flag.String("receipt", "", "install receipt path; defaults to the platform location")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := install.RunHost(ctx, *receipt, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
