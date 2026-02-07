package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/leftmike/gmcpt/client"
)

func clientCmd(cmd string, fs *flag.FlagSet, parse func() []string) {
	var apiKey, header string
	var sse, jsonFlag bool

	fs.StringVar(&apiKey, "apikey", "", "API key for remote MCP server")
	fs.StringVar(&header, "header", "", "header for API key")
	fs.BoolVar(&sse, "sse", false, "use SSE transport")
	fs.BoolVar(&jsonFlag, "json", false, "output as JSON")

	// list
	var view string
	var prompts, resources, tools bool
	if cmd == "list" {
		fs.StringVar(&view, "view", "brief", "view mode: brief, summary, or detailed")
		fs.BoolVar(&prompts, "prompts", false, "list prompts")
		fs.BoolVar(&resources, "resources", false, "list resources")
		fs.BoolVar(&tools, "tools", false, "list tools")
	}

	// prompt
	var prompt string
	if cmd == "prompt" {
		fs.StringVar(&prompt, "prompt", "", "prompt to get")
	}

	// resource
	var resource string
	if cmd == "resource" {
		fs.StringVar(&resource, "resource", "", "resource to get")
		fs.StringVar(&resource, "uri", "", "resource to get")
	}

	svrArgs, args := splitArgs(parse())
	if len(svrArgs) == 0 {
		fatalUsage("a url or a command must be specified", fs)
	}
	var url string
	if isURL(svrArgs[0]) {
		if len(svrArgs) > 1 {
			fatalUsage("arguments are not allowed with url", fs)
		}
		url = svrArgs[0]
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if cmd == "list" {
		if len(args) > 0 {
			fatalUsage("no arguments are allowed", fs)
		}
		if view != "brief" && view != "summary" && view != "detailed" {
			fatalUsage("view must be brief, summary, or detailed", fs)
		}

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
		if url != "" {
			lst, err = client.ListRemote(ctx, url, apiKey, header, sse, lstOpts)
		} else {
			lst, err = client.ListLocal(ctx, svrArgs[0], svrArgs[1:], lstOpts)
		}
		if err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}

		if jsonFlag {
			buf, err := json.MarshalIndent(lst, "", "  ")
			if err != nil {
				fatal(err.Error())
			}
			fmt.Println(string(buf))
		} else {
			if lstOpts&client.ListPrompts != 0 {
				printPromptList(lst.Prompts, view)
			}
			if lstOpts&client.ListResources != 0 {
				printResourceList(lst.Resources, view)
			}
			if lstOpts&client.ListTools != 0 {
				printToolList(lst.Tools, view)
			}
		}
	} else if cmd == "prompt" {
		if prompt == "" && len(args) == 0 {
			fatalUsage("-prompt or at least one argument is required", fs)
		}

		if prompt != "" {
			args = append([]string{prompt}, args...)
		}

		promptArgs := map[string]string{
			"type": "object",
			// XXX: need arguments
		}
		// XXX: for _, arg := range args {
		var ret *mcp.GetPromptResult
		var err error
		if url != "" {
			ret, err = client.GetPromptRemote(ctx, url, apiKey, header, sse, args[0], promptArgs)
		} else {
			ret, err = client.GetPromptLocal(ctx, svrArgs[0], svrArgs[1:], args[0], promptArgs)
		}
		if err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}

		if jsonFlag {
			buf, err := json.MarshalIndent(ret, "", "  ")
			if err != nil {
				fatal(err.Error())
			}
			fmt.Println(string(buf))
		} else {
			printPrompt(ret)
		}
	} else if cmd == "resource" {
		if resource == "" && len(args) == 0 {
			fatalUsage("-resource or at least one argument is required", fs)
		}

		if resource != "" {
			args = append([]string{resource}, args...)
		}

		// XXX: for _, arg := range args {
		var ret *mcp.ReadResourceResult
		var err error
		if url != "" {
			ret, err = client.ReadResourceRemote(ctx, url, apiKey, header, sse, args[0])
		} else {
			ret, err = client.ReadResourceLocal(ctx, svrArgs[0], svrArgs[1:], args[0])
		}
		if err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}

		if jsonFlag {
			buf, err := json.MarshalIndent(ret, "", "  ")
			if err != nil {
				fatal(err.Error())
			}
			fmt.Println(string(buf))
		} else {
			printResource(ret)
		}
	} else {
		panic(fmt.Sprintf("unexpected command: %s", cmd))
	}
}

func splitArgs(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func singleLine(s string, l int) string {
	s, _, _ = strings.Cut(s, "\n")
	rs := []rune(s)
	if len(rs) > l {
		return string(rs[:l]) + "..."
	}
	return s
}

func printWithPrefix(prefix, s string, n int) {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if i == n {
			fmt.Print(prefix)
			fmt.Println("...")
			break
		}

		fmt.Print(prefix)
		fmt.Println(strings.TrimSpace(l))
	}
}

func printPromptList(prompts []*mcp.Prompt, view string) {
	fmt.Println("---- Prompts ----")
	for _, prpt := range prompts {
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
			if view == "summary" {
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
					fmt.Printf("    %s\n", singleLine(prpt.Description, 70))
				}
			} else { // view == "detailed"
				fmt.Printf("    %s\n", prpt.Name)
				if len(prpt.Arguments) > 0 {
					for _, arg := range prpt.Arguments {
						if arg.Required {
							fmt.Printf("        %s\n", arg.Name)
						} else {
							fmt.Printf("        [%s]\n", arg.Name)
						}
						if arg.Description != "" {
							if view == "detailed" {
								printWithPrefix("        ", arg.Description, 5)
							} else { // view == "summary"
								fmt.Printf("        %s\n", singleLine(arg.Description, 60))
							}
						}
					}
				}
				if prpt.Description != "" {
					printWithPrefix("    ", prpt.Description, 5)
				}
			}
			fmt.Println()
		}
	}
}

func printResourceList(resources []*mcp.Resource, view string) {
	fmt.Println("---- Resources ----")
	for _, rsc := range resources {
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
					printWithPrefix("    ", rsc.Description, 5)
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

func printToolList(tools []*mcp.Tool, view string) {
	fmt.Println("---- Tools ----")
	for _, tl := range tools {
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
					fmt.Printf("[%s %s]", args[i], types[i])
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
					printWithPrefix("    ", tl.Description, 5)
				}
				if tl.InputSchema != nil {
					buf, err := json.MarshalIndent(tl.InputSchema, "    ", "    ")
					if err == nil {
						fmt.Printf("    %s\n", string(buf))
					}
				}
				if tl.OutputSchema != nil {
					buf, err := json.MarshalIndent(tl.OutputSchema, "    ", "    ")
					if err == nil {
						fmt.Printf("    %s\n", string(buf))
					}
				}
			}
			fmt.Println()
		}
	}
}

func printPrompt(ret *mcp.GetPromptResult) {
	if ret.Description != "" {
		fmt.Println(ret.Description)
		fmt.Println()
	}

	for _, msg := range ret.Messages {
		fmt.Printf("[%s]\n", msg.Role)
		printContent(msg.Content)
		fmt.Println()
	}
}

func printContent(cnt mcp.Content) {
	switch cnt := cnt.(type) {
	case *mcp.TextContent:
		fmt.Println(cnt.Text)
	case *mcp.ImageContent:
		fmt.Printf("[image: %s, %d bytes]\n", cnt.MIMEType, len(cnt.Data))
	case *mcp.AudioContent:
		fmt.Printf("[audio: %s, %d bytes]\n", cnt.MIMEType, len(cnt.Data))
	case *mcp.EmbeddedResource:
		if cnt.Resource != nil {
			fmt.Printf("[resource: %s]\n", cnt.Resource.URI)
			if cnt.Resource.Text != "" {
				fmt.Println(cnt.Resource.Text)
			} else if len(cnt.Resource.Blob) > 0 {
				fmt.Printf("[blob: %d bytes]\n", len(cnt.Resource.Blob))
			}
		}
	case *mcp.ResourceLink:
		fmt.Printf("[resource link: %s", cnt.URI)
		if cnt.Name != "" {
			fmt.Printf(" (%s)", cnt.Name)
		}
		fmt.Println("]")
	default:
		fmt.Printf("[unknown content type: %T]\n", cnt)
	}
}

func printResource(ret *mcp.ReadResourceResult) {
	for _, rc := range ret.Contents {
		if rc.MIMEType != "" {
			fmt.Printf("[%s] %s\n", rc.MIMEType, rc.URI)
		} else {
			fmt.Println(rc.URI)
		}
		if rc.Text != "" {
			fmt.Println(rc.Text)
		} else if len(rc.Blob) > 0 {
			fmt.Printf("[blob: %d bytes]\n", len(rc.Blob))
		}
	}
}
