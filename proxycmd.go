package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/leftmike/gmcpt/proxy"
)

func proxyCmd(fs *flag.FlagSet, parse func() ([]string, *slog.Logger)) {
	var logProto, apiKey, header string

	fs.StringVar(&logProto, "logproto", "", "protocol log file path")
	fs.StringVar(&apiKey, "apikey", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")

	args, l := parse()
	if len(args) != 1 {
		fatalUsage("one argument, the remote MCP server URL, is required", fs)
	}

	slog.Info("starting", "cmd", os.Args[0]+os.Args[1], "args", strings.Join(os.Args[2:], " "),
		"pid", os.Getpid())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := proxy.NewProxy(args[0], apiKey, header, false).Run(ctx, l, logProto)
	if err != nil && ctx.Err() == nil {
		fatal(err.Error())
	}

	slog.Info("exiting", "cmd", os.Args[0]+os.Args[1], "args", strings.Join(os.Args[2:], " "),
		"pid", os.Getpid())
}
