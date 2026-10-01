package main

import (
	"context"
	"github.com/kuopenx/pagehub/internal/cli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Environment{})
	stop()
	os.Exit(code)
}
