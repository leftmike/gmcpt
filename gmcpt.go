package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/leftmike/gmcpt/proxy"
)

func fatal(err error) {
	slog.Error(err.Error())
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", os.Args[0], os.Args[1], err)
	os.Exit(1)
}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (dh discardHandler) WithAttrs([]slog.Attr) slog.Handler     { return dh }
func (dh discardHandler) WithGroup(string) slog.Handler          { return dh }

func setupLogging(log bool, logfile string) *slog.Logger {
	var l *slog.Logger
	if log {
		if logfile != "" {
			file, err := os.OpenFile(logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s: %s", os.Args[0], os.Args[1], err)
				os.Exit(1)
			}

			l = slog.New(slog.NewTextHandler(file, nil))
		} else {
			l = slog.New(slog.NewTextHandler(os.Stderr, nil))
		}
	} else {
		l = slog.New(discardHandler{})
	}

	slog.SetDefault(l)
	return l
}

func proxyCmd() {
	var log bool
	var logfile, logProto, url, apiKey, header string

	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fs.BoolVar(&log, "log", false, "enable logging")
	fs.StringVar(&logfile, "logfile", "", "log file path")
	fs.StringVar(&logProto, "logproto", "", "protocol log file path")
	fs.StringVar(&url, "url", "", "remote MCP server URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")
	fs.Parse(os.Args[2:])

	if url == "" {
		fmt.Fprintf(os.Stderr, "%s %s: url is required\n", os.Args[0], os.Args[1])
		os.Exit(1)
	}

	l := setupLogging(log, logfile)
	slog.Info("starting", slog.String("cmd", os.Args[0]+os.Args[1]),
		slog.String("args", strings.Join(os.Args[2:], " ")), slog.Int("pid", os.Getpid()))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := proxy.NewProxy(url, apiKey, header, false).Run(ctx, l, logProto)
	if err != nil && ctx.Err() == nil {
		fatal(err)
	}

	slog.Info("exiting", slog.String("cmd", os.Args[0]+os.Args[1]),
		slog.String("args", strings.Join(os.Args[2:], " ")), slog.Int("pid", os.Getpid()))
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gmcpt proxy")
	os.Exit(1)
}

func main() {
	if len(os.Args) == 1 {
		usage()
	}

	switch os.Args[1] {
	case "proxy":
		proxyCmd()
	default:
		usage()
	}
}
