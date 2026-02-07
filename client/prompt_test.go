package client

import (
	"context"
	"fmt"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
)

func newPromptServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-prompt-server", "0.1.0",
		mcpsvr.WithPromptCapabilities(true),
	)

	tsvr.AddPrompt(mcpgo.NewPrompt("greet",
		mcpgo.WithPromptDescription("generates a greeting"),
		mcpgo.WithArgument("name",
			mcpgo.ArgumentDescription("name to greet"),
			mcpgo.RequiredArgument(),
		)),
		func(ctx context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
			name := req.Params.Arguments["name"]
			return mcpgo.NewGetPromptResult(
				"a greeting message",
				[]mcpgo.PromptMessage{
					mcpgo.NewPromptMessage(mcpgo.RoleUser,
						mcpgo.NewTextContent(fmt.Sprintf("Hello, %s!", name))),
				},
			), nil
		})

	tsvr.AddPrompt(mcpgo.NewPrompt("help",
		mcpgo.WithPromptDescription("shows help information")),
		func(ctx context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
			return mcpgo.NewGetPromptResult(
				"help information",
				[]mcpgo.PromptMessage{
					mcpgo.NewPromptMessage(mcpgo.RoleAssistant,
						mcpgo.NewTextContent("This is the help message.")),
				},
			), nil
		})

	return tsvr
}

func testGetPrompt(t *testing.T, prompt string, promptArgs map[string]string,
	expected string) {

	svr := mcpsvr.NewTestStreamableHTTPServer(newPromptServer())
	defer svr.Close()

	ret, err := GetPromptRemote(context.Background(), svr.URL+"/mcp", "", "", false, prompt,
		promptArgs)
	if err != nil {
		t.Errorf("GetPromptRemote(%s) failed: %s", prompt, err)
	} else {
		if len(ret.Messages) == 0 {
			t.Errorf("GetPromptRemote(%s) returned no messages", prompt)
		}
		if ret.Description != expected {
			t.Errorf("GetPromptRemote(%s) description got %s want %s", prompt, ret.Description,
				expected)
		}
	}
}

func TestGetPrompt(t *testing.T) {
	testGetPrompt(t, "greet", map[string]string{"name": "World"}, "a greeting message")
	testGetPrompt(t, "help", nil, "help information")
}

func TestGetPromptRemoteServers(t *testing.T) {
	cases := []struct {
		short      bool
		url        string
		sse        bool
		prompt     string
		promptArgs map[string]string
	}{
		{
			short:  true,
			url:    "https://docs.mcp.cloudflare.com/sse",
			sse:    true,
			prompt: "workers-prompt-full",
		},
		{
			short:      true,
			url:        "https://hf.co/mcp",
			prompt:     "Model Details",
			promptArgs: map[string]string{"model_id": "openai-community/gpt2"},
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "Paper Summary",
			promptArgs: map[string]string{"paper_id": "2502.16161"},
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "User Summary",
			promptArgs: map[string]string{"user_id": "clem"},
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "Dataset Details",
			promptArgs: map[string]string{"dataset_id": "squad"},
		},
	}

	for _, c := range cases {
		if testing.Short() && !c.short {
			fmt.Printf("skipping %s prompt %s\n", c.url, c.prompt)
			continue
		}

		ret, err := GetPromptRemote(context.Background(), c.url, "", "", c.sse, c.prompt,
			c.promptArgs)
		if err != nil {
			t.Errorf("GetPromptRemote(%s, %s) failed: %s", c.url, c.prompt, err)
		} else {
			if len(ret.Messages) == 0 {
				t.Errorf("GetPromptRemote(%s, %s) returned no messages", c.url, c.prompt)
			}
		}
	}
}
