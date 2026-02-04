package client

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	resourceImpl = mcp.Implementation{
		Name:    "gmcpt-resource-client",
		Version: "0.1.0",
	}
)

func ReadResourceLocal(ctx context.Context, cmd string, cmdArgs []string,
	uri string) (*mcp.ReadResourceResult, error) {

	sess, err := mcp.NewClient(&resourceImpl, nil).Connect(ctx,
		&mcp.CommandTransport{Command: exec.Command(cmd, cmdArgs...)}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	return sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
}

func ReadResourceRemote(ctx context.Context, url, apiKey, header string, sse bool,
	uri string) (*mcp.ReadResourceResult, error) {

	sm := NewSessionManager(url, apiKey, header, sse)
	defer sm.Close()

	var ret *mcp.ReadResourceResult
	err := sm.WithSession(ctx,
		mcp.NewClient(&resourceImpl, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			ret, err = sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
			return err
		})
	return ret, err
}
