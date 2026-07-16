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
	if len(os.Args) > 1 && os.Args[1] == "__sign" {
		executable, err := os.Executable()
		if err == nil {
			err = install.RunSigner(context.Background(), "", executable, os.Stdin, os.Stdout)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	receipt := flag.String("receipt", "", "install receipt path; defaults to the platform location")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := install.RunHost(ctx, *receipt, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
