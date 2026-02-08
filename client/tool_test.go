package client

import (
	"context"
	"fmt"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newToolServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-tool-server", "0.1.0", mcpsvr.WithToolCapabilities(true))

	tsvr.AddTool(mcpgo.NewTool("echo",
		mcpgo.WithDescription("echoes back the input"),
		mcpgo.WithString("message",
			mcpgo.Required(),
			mcpgo.Description("message to echo"),
		)),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			msg := req.GetString("message", "")
			return mcpgo.NewToolResultText(fmt.Sprintf("echo: %s", msg)), nil
		})

	tsvr.AddTool(mcpgo.NewTool("add",
		mcpgo.WithDescription("adds two numbers"),
		mcpgo.WithNumber("a",
			mcpgo.Required(),
			mcpgo.Description("first number"),
		),
		mcpgo.WithNumber("b",
			mcpgo.Required(),
			mcpgo.Description("second number"),
		)),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			a := req.GetFloat("a", 0)
			b := req.GetFloat("b", 0)
			return mcpgo.NewToolResultText(fmt.Sprintf("sum: %g", a+b)), nil
		})

	return tsvr
}

func testCallTool(t *testing.T, name string, args map[string]any, expected string) {
	svr := mcpsvr.NewTestStreamableHTTPServer(newToolServer())
	defer svr.Close()

	ret, err := CallToolRemote(context.Background(), svr.URL+"/mcp", "", "", false, name, args)
	if err != nil {
		t.Errorf("CallToolRemote(%s) failed: %s", name, err)
	} else if len(ret.Content) == 0 {
		t.Errorf("CallToolRemote(%s) returned no content", name)
	} else {
		tc, ok := ret.Content[0].(*mcp.TextContent)
		if !ok {
			t.Errorf("CallToolRemote(%s) expected *mcp.TextContent, got %T", name, ret.Content[0])
		} else if tc.Text != expected {
			t.Errorf("CallToolRemote(%s) got %s want %s", name, tc.Text, expected)
		}
	}
}

func TestCallTool(t *testing.T) {
	testCallTool(t, "echo", map[string]any{"message": "hello world"}, "echo: hello world")
	testCallTool(t, "add", map[string]any{"a": 3.5, "b": 2.5}, "sum: 6")
}

func TestCallToolRemoteServers(t *testing.T) {
	cases := []struct {
		short bool
		url   string
		sse   bool
		tool  string
		args  map[string]any
		text  string
	}{
		{
			short: true,
			url:   "https://docs.mcp.cloudflare.com/sse",
			sse:   true,
			tool:  "search_cloudflare_documentation",
			args:  map[string]any{"query": "workers"},
			text:  "<result>\n<url>https://developers.cloudflare.com/https://developers.cloudflare.com/workers/observability/exporting-opente",
		},
		{
			short: true,
			url:   "https://hf.co/mcp",
			tool:  "model_search",
			args:  map[string]any{"query": "gpt2", "limit": 1},
			text:  "Showing first 1 models matching query \"gpt2\":\n\n## openai-community/gpt2-large\n\n**Task:** text-generation | **Library:** ",
		},
		{
			url:  "https://hf.co/mcp",
			tool: "paper_search",
			args: map[string]any{"query": "attention is all you need", "limit": 1},
			text: "75 papers matched the query 'attention is all you need'. Here are the first 12 results.\n\n---\n\n## Attention Is All You Ne",
		},
		{
			url:  "https://hf.co/mcp",
			tool: "dataset_search",
			args: map[string]any{"query": "squad", "limit": 1},
			text: "Showing first 1 datasets matching query \"squad\":\n\n## rajpurkar/squad\n\n\n\t\n\t\t\n\t\tDataset Card for SQuAD\n\t\n\n\n\t\n\t\t\n\t\tDataset ",
		},
	}

	for _, c := range cases {
		if testing.Short() && !c.short {
			fmt.Printf("skipping %s tool %s\n", c.url, c.tool)
			continue
		}

		ret, err := CallToolRemote(context.Background(), c.url, "", "", c.sse, c.tool, c.args)
		if err != nil {
			t.Errorf("CallToolRemote(%s, %s) failed: %s", c.url, c.tool, err)
		} else if len(ret.Content) == 0 {
			t.Errorf("CallToolRemote(%s, %s) returned no content", c.url, c.tool)
		} else {
			tc, ok := ret.Content[0].(*mcp.TextContent)
			if !ok {
				t.Errorf("CallToolRemote(%s, %s) expected *mcp.TextContent, got %T",
					c.url, c.tool, ret.Content[0])
			} else {
				got := tc.Text
				if len(got) > 120 {
					got = got[:120]
				}
				if got != c.text {
					t.Errorf("CallToolRemote(%s, %s) got %q want %q",
						c.url, c.tool, got, c.text)
				}
			}
		}
	}
}
