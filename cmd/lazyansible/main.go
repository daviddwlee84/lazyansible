package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/daviddwlee84/lazyansible/internal/cli"
)

func main() { os.Exit(run()) }
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
