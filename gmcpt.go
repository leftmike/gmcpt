/*
To Do:
- prompt command to fetch a prompt
- resource command to read a resource
- tool command to call a tool

- list command: print prompt arguments in summary and detailed views
*/
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func fatal(msg string) {
	slog.Error(msg)
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", os.Args[0], os.Args[1], msg)
	os.Exit(1)
}

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
		l = slog.New(slog.DiscardHandler)
	}

	slog.SetDefault(l)
	return l
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gmcpt <proxy | list>")
	os.Exit(1)
}

func main() {
	if len(os.Args) == 1 {
		usage()
	}

	var log bool
	var logfile string
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fs.BoolVar(&log, "log", false, "enable logging")
	fs.StringVar(&logfile, "logfile", "", "log file path")

	parse := func() ([]string, *slog.Logger) {
		fs.Parse(os.Args[2:])
		return fs.Args(), setupLogging(log, logfile)
	}

	switch os.Args[1] {
	case "proxy":
		proxyCmd(fs, parse)
	case "list":
		listCmd(fs, parse)
	default:
		usage()
	}
}
