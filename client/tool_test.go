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
	tsvr := mcpsvr.NewMCPServer("test-tool-server", "0.1.0", mcpsvr.WithToolCapabilities(true),
		mcpsvr.WithElicitation())
	tsvr.EnableSampling()

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

	tsvr.AddTool(mcpgo.NewTool("do-sampling",
		mcpgo.WithDescription("calls CreateMessage (sampling) and returns the result")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			ret, err := tsvr.RequestSampling(ctx, mcpgo.CreateMessageRequest{
				CreateMessageParams: mcpgo.CreateMessageParams{
					Messages: []mcpgo.SamplingMessage{{
						Role:    mcpgo.RoleUser,
						Content: mcpgo.TextContent{Type: "text", Text: "hello from tool"},
					}},
					MaxTokens: 100,
				},
			})
			if err != nil {
				return nil, err
			}

			tc, ok := ret.Content.(mcpgo.TextContent)
			if !ok {
				return nil, fmt.Errorf("sampling: want text got %#v", ret.Content)
			}
			return mcpgo.NewToolResultText("sampled: " + tc.Text), nil
		})

	tsvr.AddTool(mcpgo.NewTool("do-elicitation",
		mcpgo.WithDescription("calls Elicit and returns the action and content")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			ret, err := tsvr.RequestElicitation(ctx, mcpgo.ElicitationRequest{
				Params: mcpgo.ElicitationParams{
					Message: "please provide your name",
					RequestedSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
						},
					},
				},
			})
			if err != nil {
				return nil, err
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("action: %s", ret.Action)), nil
		})

	return tsvr
}

func testCallTool(t *testing.T, name string, args map[string]any, expected string) {
	svr := mcpsvr.NewTestStreamableHTTPServer(newToolServer())
	defer svr.Close()

	ret, cmr, er, err := CallToolRemote(context.Background(), svr.URL+"/mcp", "", "", false, name,
		args)
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

		if cmr != nil {
			t.Errorf("CallToolRemote(%s) unexpected summarization: %v", name, cmr)
		}
		if er != nil {
			t.Errorf("CallToolRemote(%s) unexpected elicitation: %v", name, er)
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
			text:  "<result>\n<url>https://developers.cloudflare.com/https://developers.cloudflare.com/workers-ai/features/function-calling/e",
		},
		{
			short: true,
			url:   "https://hf.co/mcp",
			tool:  "model_search",
			args:  map[string]any{"query": "gpt2", "limit": 1},
			text:  "Found 1 repositories across models matching query \"gpt2\".\n\n## Models (1)\n\n### openai-community/gpt2\n\n**Task:** text-gene",
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
			text: "Found 1 repositories across datasets matching query \"squad\".\n\n## Datasets (1)\n\n### iapp/iapp_wiki_qa_squad\n\n`iapp_wiki_q",
		},
	}

	for _, c := range cases {
		if testing.Short() && !c.short {
			fmt.Printf("skipping %s tool %s\n", c.url, c.tool)
			continue
		}

		ret, cmr, er, err := CallToolRemote(context.Background(), c.url, "", "", c.sse, c.tool,
			c.args)
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

			if cmr != nil {
				t.Errorf("CallToolRemote(%s, %s) unexpected summarization: %v", c.url, c.tool, cmr)
			}
			if er != nil {
				t.Errorf("CallToolRemote(%s, %s) unexpected elicitation: %v", c.url, c.tool, er)
			}
		}
	}
}

func TestCallToolSampling(t *testing.T) {
	svr := mcpsvr.NewTestStreamableHTTPServer(newToolServer(), mcpsvr.WithStateful(true))
	defer svr.Close()

	tool := "do-sampling"
	ret, cmr, er, err := CallToolRemote(context.Background(), svr.URL+"/mcp", "", "", false, tool,
		nil)
	if err != nil {
		t.Errorf("CallToolRemote(%s) failed: %s", tool, err)
	} else if len(ret.Content) == 0 {
		t.Errorf("CallToolRemote(%s) returned no content", tool)
	} else {
		tc, ok := ret.Content[0].(*mcp.TextContent)
		if !ok {
			t.Errorf("CallToolRemote(%s) expected *mcp.TextContent, got %T", tool, ret.Content[0])
		} else {
			want := "sampled: no response"
			if tc.Text != want {
				t.Errorf("CallToolRemote(%s) got %q want %q", tool, tc.Text, want)
			}
		}

		if cmr == nil {
			t.Errorf("CallToolRemote(%ss) missing summarization", tool)
		}
		if er != nil {
			t.Errorf("CallToolRemote(%s) unexpected elicitation: %v", tool, er)
		}
	}
}

func TestCallToolElicitation(t *testing.T) {
	svr := mcpsvr.NewTestStreamableHTTPServer(newToolServer(), mcpsvr.WithStateful(true))
	defer svr.Close()

	tool := "do-elicitation"
	ret, cmr, er, err := CallToolRemote(context.Background(), svr.URL+"/mcp", "", "", false, tool,
		nil)
	if err != nil {
		t.Errorf("CallToolRemote(%s) failed: %s", tool, err)
	} else if len(ret.Content) == 0 {
		t.Errorf("CallToolRemote(%s) returned no content", tool)
	} else {
		tc, ok := ret.Content[0].(*mcp.TextContent)
		if !ok {
			t.Errorf("CallToolRemote(%s) expected *mcp.TextContent, got %T", tool, ret.Content[0])
		} else {
			want := "action: accept"
			if tc.Text != want {
				t.Errorf("CallToolRemote(%s) got %q want %q", tool, tc.Text, want)
			}
		}

		if cmr != nil {
			t.Errorf("CallToolRemote(%ss) unexpected summarization: %v", tool, cmr)
		}
		if er == nil {
			t.Errorf("CallToolRemote(%s) missing elicitation", tool)
		}
	}
}
