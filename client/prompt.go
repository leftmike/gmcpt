package client

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	promptImpl = mcp.Implementation{
		Name:    "gmcpt-prompt-client",
		Version: "0.1.0",
	}
)

func GetPromptLocal(ctx context.Context, cmd string, cmdArgs []string, prompt string,
	promptArgs map[string]string) (*mcp.GetPromptResult, error) {

	sess, err := mcp.NewClient(&promptImpl, nil).Connect(ctx,
		&mcp.CommandTransport{Command: exec.Command(cmd, cmdArgs...)}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	return sess.GetPrompt(ctx, &mcp.GetPromptParams{Name: prompt, Arguments: promptArgs})
}

func GetPromptRemote(ctx context.Context, url, apiKey, header string, sse bool, prompt string,
	promptArgs map[string]string) (*mcp.GetPromptResult, error) {

	sm := NewSessionManager(url, apiKey, header, sse)
	defer sm.Close()

	var ret *mcp.GetPromptResult
	err := sm.WithSession(ctx,
		mcp.NewClient(&promptImpl, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			ret, err = sess.GetPrompt(ctx,
				&mcp.GetPromptParams{Name: prompt, Arguments: promptArgs})
			return err
		})
	return ret, err
}
