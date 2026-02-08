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
