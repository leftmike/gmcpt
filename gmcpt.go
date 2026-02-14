/*
To Do:
- tool_test: https://github.com/leftmike/research/blob/main/mcplist/successful_servers.txt
*/
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

type stringArgs []string

func (sa *stringArgs) String() string {
	return strings.Join(*sa, ", ")
}

func (sa *stringArgs) Set(val string) error {
	*sa = append(*sa, val)
	return nil
}

func fatalUsage(msg string, fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", os.Args[0], os.Args[1], msg)
	cmdUsage(fs)
}

func cmdUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, "Usage of %s %s:\n", os.Args[0], os.Args[1])
	fs.VisitAll(func(flg *flag.Flag) {
		fmt.Fprintf(os.Stderr, "    -%s", flg.Name)
		name, usage := flag.UnquoteUsage(flg)
		if name != "" {
			fmt.Fprintf(os.Stderr, " %s", name)
		}
		if len(flg.Name) == 1 {
			fmt.Fprint(os.Stderr, "\t")
		} else {
			fmt.Fprint(os.Stderr, "\n    \t")
		}
		fmt.Fprintln(os.Stderr, strings.ReplaceAll(usage, "\n", "\n    \t"))
	})
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage of gmcpt:
gmcpt <command> [arguments]

Commands:
    list        list prompts, resources, and/or tools
    prompt      get a prompt
    proxy       local proxy for a remote mcp server
    resource    get a resource
    tool        call a tool`)
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

	parseLogger := func() ([]string, *slog.Logger) {
		fs.Usage = func() {
			cmdUsage(fs)
		}
		fs.Parse(os.Args[2:])
		return fs.Args(), setupLogging(log, logfile)
	}
	parse := func() []string {
		args, _ := parseLogger()
		return args
	}

	switch os.Args[1] {
	case "list", "prompt", "resource", "tool":
		clientCmd(os.Args[1], fs, parse)
	case "proxy":
		proxyCmd(fs, parseLogger)
	default:
		usage()
	}
}
