package client

import (
	"context"
	"slices"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
)

func newAllCapsServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-list-server", "0.1.0",
		mcpsvr.WithToolCapabilities(true),
		mcpsvr.WithPromptCapabilities(true),
		mcpsvr.WithResourceCapabilities(false, false),
	)

	tsvr.AddTool(mcpgo.NewTool("echo",
		mcpgo.WithDescription("echoes back the input")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("echo"), nil
		})

	tsvr.AddTool(mcpgo.NewTool("add",
		mcpgo.WithDescription("adds two numbers")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("sum"), nil
		})

	tsvr.AddPrompt(mcpgo.NewPrompt("greet",
		mcpgo.WithPromptDescription("generates a greeting")),
		func(ctx context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
			return mcpgo.NewGetPromptResult("greeting",
				[]mcpgo.PromptMessage{
					mcpgo.NewPromptMessage(mcpgo.RoleUser,
						mcpgo.NewTextContent("Hello!")),
				},
			), nil
		})

	tsvr.AddResource(
		mcpgo.NewResource("file:///test.txt", "test.txt",
			mcpgo.WithResourceDescription("test file")),
		func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents,
			error) {

			return []mcpgo.ResourceContents{
				mcpgo.TextResourceContents{URI: "file:///test.txt", Text: "test"},
			}, nil
		})

	return tsvr
}

func TestListAll(t *testing.T) {
	tsvr := newAllCapsServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	url := svr.URL + "/mcp"
	lst, err := ListRemote(context.Background(), url, "", "", false,
		ListTools|ListPrompts|ListResources)
	if err != nil {
		t.Errorf("ListRemote(%s) failed with %s", url, err)
	} else {
		if len(lst.Tools) != 2 {
			t.Errorf("ListRemote(%s) got %d tools want 2", url, len(lst.Tools))
		}
		if len(lst.Prompts) != 1 {
			t.Errorf("ListRemote(%s) got %d prompts want 1", url, len(lst.Prompts))
		}
		if len(lst.Resources) != 1 {
			t.Errorf("ListRemote(%s) got %d resources want 1", url, len(lst.Resources))
		}

		toolNames := []string{"echo", "add"}
		found := map[string]struct{}{}
		for _, tool := range lst.Tools {
			if !slices.Contains(toolNames, tool.Name) {
				t.Errorf("ListRemote(%s) unexpected tool: %s", url, tool.Name)
			} else {
				found[tool.Name] = struct{}{}
			}
		}
		for _, name := range toolNames {
			if _, ok := found[name]; !ok {
				t.Errorf("ListRemote(%s) missing %s tool", url, name)
			}
		}

		if lst.Prompts[0].Name != "greet" {
			t.Errorf("ListRemote(%s) got prompt %s want greet", url, lst.Prompts[0].Name)
		}
	}
}

func TestListSelectiveOptions(t *testing.T) {
	cases := []struct {
		opts      ListOptions
		tools     int
		prompts   int
		resources int
	}{
		{opts: ListTools, tools: 2},
		{opts: ListPrompts, prompts: 1},
		{opts: ListResources, resources: 1},
		{opts: ListTools | ListPrompts, tools: 2, prompts: 1},
		{opts: ListTools | ListResources, tools: 2, resources: 1},
		{opts: ListPrompts | ListResources, prompts: 1, resources: 1},
	}

	tsvr := newAllCapsServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	ctx := context.Background()
	for _, c := range cases {
		url := svr.URL + "/mcp"
		lst, err := ListRemote(ctx, url, "", "", false, c.opts)
		if err != nil {
			t.Errorf("ListRemote(%s, %v) failed with %s", url, c.opts, err)
			continue
		}

		if len(lst.Tools) != c.tools {
			t.Errorf("ListRemote(%s, %v) got %d tools want %d", url, c.opts, len(lst.Tools),
				c.tools)
		}
		if len(lst.Prompts) != c.prompts {
			t.Errorf("ListRemote(%s, %v) got %d prompts want %d", url, c.opts, len(lst.Prompts),
				c.prompts)
		}
		if len(lst.Resources) != c.resources {
			t.Errorf("ListRemote(%s, %v) got %d resources want %d", url, c.opts,
				len(lst.Resources), c.resources)
		}
	}
}

func TestListCapabilitiesFilter(t *testing.T) {
	tsvr := mcpsvr.NewMCPServer("tools-only-server", "0.1.0",
		mcpsvr.WithToolCapabilities(true))
	tsvr.AddTool(mcpgo.NewTool("echo",
		mcpgo.WithDescription("echoes")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("echo"), nil
		})

	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	url := svr.URL + "/mcp"
	lst, err := ListRemote(context.Background(), url, "", "", false,
		ListTools|ListPrompts|ListResources)
	if err != nil {
		t.Errorf("ListRemote(%s) failed with %s", url, err)
	} else {
		if len(lst.Tools) != 1 {
			t.Errorf("ListRemote(%s) got %d tools want 1", url, len(lst.Tools))
		}
		if len(lst.Prompts) != 0 {
			t.Errorf("ListRemote(%s) got %d prompts want 0", url, len(lst.Prompts))
		}
		if len(lst.Resources) != 0 {
			t.Errorf("ListRemote(%s) got %d resources want 0", url, len(lst.Resources))
		}
	}
}
