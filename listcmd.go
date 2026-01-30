package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/leftmike/gmcpt/list"
)

func listCmd() {
	var logFlag bool
	var logfile, url, apiKey, header string
	var sse, verbose, tools, prompts, resources, asJSON bool

	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fs.BoolVar(&logFlag, "log", false, "enable logging")
	fs.StringVar(&logfile, "logfile", "", "log file path")
	fs.StringVar(&url, "url", "", "remote MCP server URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")
	fs.BoolVar(&sse, "sse", false, "use SSE transport")
	fs.BoolVar(&verbose, "v", false, "verbose text output")
	fs.BoolVar(&tools, "tools", false, "list tools")
	fs.BoolVar(&prompts, "prompts", false, "list prompts")
	fs.BoolVar(&resources, "resources", false, "list resources")
	fs.BoolVar(&asJSON, "json", false, "output as JSON")
	fs.Parse(os.Args[2:])

	args := fs.Args()
	if url == "" && len(args) == 0 {
		fmt.Fprintf(os.Stderr, "%s %s: specify -url or a command\n", os.Args[0], os.Args[1])
		os.Exit(1)
	}
	if url != "" && len(args) > 0 {
		fmt.Fprintf(os.Stderr, "%s %s: specify -url or a command, not both\n",
			os.Args[0], os.Args[1])
		os.Exit(1)
	}

	setupLogging(logFlag, logfile)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var lstOpts list.ListOptions
	if tools {
		lstOpts |= list.ListOptionsTools
	}
	if prompts {
		lstOpts |= list.ListOptionsPrompts
	}
	if resources {
		lstOpts |= list.ListOptionsResources
	}

	var lst *list.ListOutput
	var err error
	if len(args) > 0 {
		lst, err = list.Local(ctx, args[0], args[1:], lstOpts)
	} else {
		lst, err = list.Remote(ctx, url, apiKey, header, sse, lstOpts)
	}
	if err != nil && ctx.Err() == nil {
		fatal(err)
	}

	if asJSON {
		printJSON(lst)
	} else {
		printText(lst, verbose)
	}
}

func printJSON(lst *list.ListOutput) {
	buf, err := json.MarshalIndent(lst, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(buf))
}

func printText(lst *list.ListOutput, verbose bool) {
	if len(lst.Tools) > 0 {
		fmt.Println("Tools:")
		for _, t := range lst.Tools {
			fmt.Printf("  %s\n", t.Name)
			if !verbose {
				continue
			}
			if t.Description != "" {
				fmt.Printf("    %s\n", t.Description)
			}
			if t.InputSchema != nil {
				buf, err := json.Marshal(t.InputSchema)
				if err == nil && string(buf) != "{}" {
					fmt.Printf("    Input: %s\n", buf)
				}
			}
		}
	}

	if len(lst.Prompts) > 0 {
		fmt.Println("Prompts:")
		for _, p := range lst.Prompts {
			fmt.Printf("  %s\n", p.Name)
			if !verbose {
				continue
			}
			if p.Description != "" {
				fmt.Printf("    %s\n", p.Description)
			}
			if len(p.Arguments) > 0 {
				fmt.Println("    Arguments:")
				for _, a := range p.Arguments {
					req := ""
					if a.Required {
						req = " (required)"
					}
					desc := ""
					if a.Description != "" {
						desc = " - " + a.Description
					}
					fmt.Printf("      %s%s%s\n", a.Name, req, desc)
				}
			}
		}
	}

	if len(lst.Resources) > 0 {
		fmt.Println("Resources:")
		for _, r := range lst.Resources {
			fmt.Printf("  %s (%s)\n", r.Name, r.URI)
			if !verbose {
				continue
			}
			if r.Description != "" {
				fmt.Printf("    %s\n", r.Description)
			}
			if r.MIMEType != "" {
				fmt.Printf("    MIME: %s\n", r.MIMEType)
			}
		}
	}
}
