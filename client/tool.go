package client

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	toolImpl = mcp.Implementation{
		Name:    "gmcpt-tool-client",
		Version: "0.1.0",
	}
)

func CallToolLocal(ctx context.Context, cmd string, cmdArgs []string, tool string,
	toolArgs map[string]any) (*mcp.CallToolResult, error) {

	sess, err := mcp.NewClient(&toolImpl, nil).Connect(ctx,
		&mcp.CommandTransport{Command: exec.Command(cmd, cmdArgs...)}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	return sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: toolArgs})
}

func CallToolRemote(ctx context.Context, url, apiKey, header string, sse bool, tool string,
	toolArgs map[string]any) (*mcp.CallToolResult, error) {

	sm := NewSessionManager(url, apiKey, header, sse)
	defer sm.Close()

	var ret *mcp.CallToolResult
	err := sm.WithSession(ctx, mcp.NewClient(&toolImpl, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			ret, err = sess.CallTool(ctx,
				&mcp.CallToolParams{Name: tool, Arguments: toolArgs})
			return err
		})
	return ret, err
}
