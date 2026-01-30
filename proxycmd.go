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
	var logProto, url, apiKey, header string

	fs.StringVar(&logProto, "logproto", "", "protocol log file path")
	fs.StringVar(&url, "url", "", "remote MCP server URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")

	args, l := parse()
	if len(args) != 0 {
		fatal("no arguments are allowed")
	} else if url == "" {
		fatal("url is required")
	}

	slog.Info("starting", "cmd", os.Args[0]+os.Args[1], "args", strings.Join(os.Args[2:], " "),
		"pid", os.Getpid())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := proxy.NewProxy(url, apiKey, header, false).Run(ctx, l, logProto)
	if err != nil && ctx.Err() == nil {
		fatal(err.Error())
	}

	slog.Info("exiting", "cmd", os.Args[0]+os.Args[1], "args", strings.Join(os.Args[2:], " "),
		"pid", os.Getpid())
}
