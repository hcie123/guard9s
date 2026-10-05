package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hcie123/guard9s/internal/app"
)

var version = "dev"
var commit = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.NewCommand(version + " (commit " + commit + ")").ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "guard9s:", err)
		os.Exit(1)
	}
}
