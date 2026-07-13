package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	launcherRuntime "github.com/PerishCode/open-cut/internal/launcher"
)

func main() {
	role := flag.String("role", "b0", "launcher role: b0 or l1")
	bootstrap := flag.String("bootstrap", "", "bootstrap.json path for b0")
	manifest := flag.String("manifest", "", "release manifest path for l1")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	switch *role {
	case "b0":
		if *bootstrap == "" {
			fmt.Fprintln(os.Stderr, "--bootstrap is required for b0")
			os.Exit(2)
		}
		err = launcherRuntime.RunB0(ctx, launcherRuntime.B0Options{BootstrapPath: *bootstrap, Stdout: os.Stdout, Stderr: os.Stderr})
	case "l1":
		if *manifest == "" {
			fmt.Fprintln(os.Stderr, "--manifest is required for l1")
			os.Exit(2)
		}
		err = launcherRuntime.RunL1(ctx, launcherRuntime.L1Options{ManifestPath: *manifest, Stdout: os.Stdout, Stderr: os.Stderr})
	default:
		fmt.Fprintf(os.Stderr, "unknown launcher role %q\n", *role)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, launcherRuntime.ErrRecoveryRequired) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(20)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
