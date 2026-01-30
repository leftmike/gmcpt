package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/leftmike/gmcpt/client"
)

func listCmd(fs *flag.FlagSet, parse func() ([]string, *slog.Logger)) {
	var url, apiKey, header string
	var sse, verbose, tools, prompts, resources, json bool

	fs.StringVar(&url, "url", "", "remote MCP server URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")
	fs.BoolVar(&sse, "sse", false, "use SSE transport")
	fs.BoolVar(&verbose, "v", false, "verbose text output")
	fs.BoolVar(&tools, "tools", false, "list tools")
	fs.BoolVar(&prompts, "prompts", false, "list prompts")
	fs.BoolVar(&resources, "resources", false, "list resources")
	fs.BoolVar(&json, "json", false, "output as JSON")

	args, _ := parse()
	if (url == "" && len(args) == 0) || (url != "" && len(args) > 0) {
		fatal("exactly one of -url or a command must be specified")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var lstOpts client.ListOptions
	if tools {
		lstOpts |= client.ListOptionsTools
	}
	if prompts {
		lstOpts |= client.ListOptionsPrompts
	}
	if resources {
		lstOpts |= client.ListOptionsResources
	}

	var lst *client.ListOutput
	var err error
	if len(args) > 0 {
		lst, err = client.ListLocal(ctx, args[0], args[1:], lstOpts)
	} else {
		lst, err = client.ListRemote(ctx, url, apiKey, header, sse, lstOpts)
	}
	if err != nil && ctx.Err() == nil {
		fatal(err.Error())
	}

	if json {
		listJSON(lst)
	} else {
		listText(lst, verbose)
	}
}

func listJSON(lst *client.ListOutput) {
	buf, err := json.MarshalIndent(lst, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(string(buf))
}

func listText(lst *client.ListOutput, verbose bool) {
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
