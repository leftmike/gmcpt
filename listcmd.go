package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/leftmike/gmcpt/client"
)

func listCmd(fs *flag.FlagSet, parse func() ([]string, *slog.Logger)) {
	var url, apiKey, header, view string
	var sse, prompts, resources, tools, json bool

	fs.StringVar(&url, "url", "", "remote MCP server URL")
	fs.StringVar(&apiKey, "api-key", "", "API key for remote server")
	fs.StringVar(&header, "header", "", "header for API key")
	fs.BoolVar(&sse, "sse", false, "use SSE transport")
	fs.StringVar(&view, "view", "brief", "view mode: brief, summary, or detailed")
	fs.BoolVar(&prompts, "prompts", false, "list prompts")
	fs.BoolVar(&resources, "resources", false, "list resources")
	fs.BoolVar(&tools, "tools", false, "list tools")
	fs.BoolVar(&json, "json", false, "output as JSON")

	args, _ := parse()
	if view != "brief" && view != "summary" && view != "detailed" {
		fatal("view must be brief, summary, or detailed")
	}
	if (url == "" && len(args) == 0) || (url != "" && len(args) > 0) {
		fatal("exactly one of -url or a command must be specified")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var lstOpts client.ListOptions
	if prompts {
		lstOpts |= client.ListPrompts
	}
	if resources {
		lstOpts |= client.ListResources
	}
	if tools {
		lstOpts |= client.ListTools
	}
	if lstOpts == 0 {
		lstOpts = client.ListTools | client.ListPrompts | client.ListResources
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
		printJSONList(lst)
	} else {
		if lstOpts&client.ListPrompts != 0 {
			printPromptList(lst, view)
		}
		if lstOpts&client.ListResources != 0 {
			printResourceList(lst, view)
		}
		if lstOpts&client.ListTools != 0 {
			printToolList(lst, view)
		}
	}
}

func printJSONList(lst *client.ListOutput) {
	buf, err := json.MarshalIndent(lst, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(string(buf))
}
func singleLine(s string, l int) string {
	s, _, _ = strings.Cut(s, "\n")
	rs := []rune(s)
	if len(rs) > l {
		return string(rs[:l])
	}
	return s
}

func printWithPrefix(prefix, s string) {
	lines := strings.Split(s, "\n")
	for _, l := range lines {
		fmt.Print(prefix)
		fmt.Println(l)
	}
}

func printPromptList(lst *client.ListOutput, view string) {
	fmt.Println("---- Prompts ----")
	for _, prpt := range lst.Prompts {
		if view == "brief" {
			if prpt.Title != "" {
				fmt.Printf("    %s (%s)\n", prpt.Title, prpt.Name)
			} else {
				fmt.Printf("    %s\n", prpt.Name)
			}
		} else { // view == "summary" || view == "detailed"
			if prpt.Title != "" {
				fmt.Printf("    %s\n", prpt.Title)
			}
			fmt.Printf("    %s", prpt.Name)
			if len(prpt.Arguments) > 0 {
				fmt.Print("(")
				for i, arg := range prpt.Arguments {
					if i > 0 {
						fmt.Print(", ")
					}
					if arg.Required {
						fmt.Print(arg.Name)
					} else {
						fmt.Printf("[%s]", arg.Name)
					}
				}
				fmt.Print(")")
			}
			fmt.Println()
			if prpt.Description != "" {
				if view == "detailed" {
					printWithPrefix("    ", prpt.Description)
				} else { // view == "summary"
					fmt.Printf("    %s\n", singleLine(prpt.Description, 70))
				}
			}
			fmt.Println()
		}
	}
}

func printResourceList(lst *client.ListOutput, view string) {
	fmt.Println("---- Resources ----")
	for _, rsc := range lst.Resources {
		if view == "brief" {
			if rsc.Title != "" {
				fmt.Printf("    %s (%s)\n", rsc.Title, rsc.Name)
			} else {
				fmt.Printf("    %s\n", rsc.Name)
			}
		} else { // view == "summary" || view == "detailed"
			if rsc.Title != "" {
				fmt.Printf("    %s\n", rsc.Title)
			}
			fmt.Printf("    %s", rsc.Name)
			if rsc.Size > 0 {
				fmt.Printf(" %d", rsc.Size)
			}
			if rsc.MIMEType != "" {
				fmt.Printf(" %s", rsc.MIMEType)
			}
			fmt.Println()
			if rsc.URI != "" {
				fmt.Printf("    %s\n", rsc.URI)
			}
			if rsc.Description != "" {
				if view == "detailed" {
					printWithPrefix("    ", rsc.Description)
				} else { // view == "summary"
					fmt.Printf("    %s\n", singleLine(rsc.Description, 70))
				}
			}
			fmt.Println()
		}
	}
}

func schemaToArgs(sch any) ([]string, []string, []bool) {
	schema, ok := sch.(map[string]any)
	if !ok {
		return nil, nil, nil
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, nil, nil
	}

	names := make([]string, 0, len(props))
	types := make([]string, 0, len(props))
	for k, v := range props {
		names = append(names, k)

		typ := "any"
		if val, ok := v.(map[string]any); ok {
			if s, ok := val["type"].(string); ok {
				typ = s
				if s == "array" {
					if items, ok := val["items"].(map[string]any); ok {
						if s, ok := items["type"].(string); ok {
							typ = s + "[]"
						}
					}
				}
			}
		}
		types = append(types, typ)
	}

	required := make([]bool, len(names))
	if val, ok := schema["required"].([]any); ok {
		for _, item := range val {
			if name, ok := item.(string); ok {
				if i := slices.Index(names, name); i >= 0 {
					required[i] = true
				}
			}
		}
	}

	return names, types, required
}

func printToolList(lst *client.ListOutput, view string) {
	fmt.Println("---- Tools ----")
	for _, tl := range lst.Tools {
		if view == "brief" {
			if tl.Title != "" {
				fmt.Printf("    %s (%s)\n", tl.Title, tl.Name)
			} else if tl.Annotations != nil && tl.Annotations.Title != "" {
				fmt.Printf("    %s (%s)\n", tl.Annotations.Title, tl.Name)
			} else {
				fmt.Printf("    %s\n", tl.Name)
			}
		} else { // view == "summary" || view == "detailed"
			if tl.Title != "" {
				fmt.Printf("    %s\n", tl.Title)
			}
			if tl.Annotations != nil && tl.Annotations.Title != "" {
				fmt.Printf("    %s\n", tl.Annotations.Title)
			}
			fmt.Printf("    %s(", tl.Name)
			args, types, required := schemaToArgs(tl.InputSchema)
			for i := range args {
				if i > 0 {
					fmt.Print(", ")
				}
				if required[i] {
					fmt.Printf("%s %s", args[i], types[i])
				} else {
					fmt.Printf("{%s %s}", args[i], types[i])
				}
			}
			// XXX tl.OutputSchema
			fmt.Println(")")
			if view == "summary" {
				if tl.Description != "" {
					fmt.Printf("    %s\n", singleLine(tl.Description, 70))
				}
			} else { // view == "detailed"
				if tl.Description != "" {
					printWithPrefix("    ", tl.Description)
				}

				// XXX: pretty print InputSchema
				// XXX: pretty print OutputSchema

				/*
					if tl.InputSchema != nil {
						buf, err := json.Marshal(tl.InputSchema)
						if err == nil && string(buf) != "{}" {
							fmt.Printf("    Input: %s\n", buf)
						}
					}
					if tl.OutputSchema != nil {
						buf, err := json.Marshal(tl.OutputSchema)
						if err == nil && string(buf) != "{}" {
							fmt.Printf("    Output: %s\n", buf)
						}
					}
				*/
			}
			fmt.Println()
		}
	}
}
