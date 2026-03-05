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
	toolArgs map[string]any) (*mcp.CallToolResult, *mcp.CreateMessageRequest,
	*mcp.ElicitRequest, error) {

	var th toolHandler
	clntOpts := mcp.ClientOptions{
		CreateMessageHandler: th.samplingHandler,
		ElicitationHandler:   th.elicitationHandler,
	}

	sess, err := mcp.NewClient(&toolImpl, &clntOpts).Connect(ctx,
		&mcp.CommandTransport{Command: exec.Command(cmd, cmdArgs...)}, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	req, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: toolArgs})
	return req, th.cmr, th.er, err
}

func CallToolRemote(ctx context.Context, url, apiKey, header string, sse bool, tool string,
	toolArgs map[string]any) (*mcp.CallToolResult, *mcp.CreateMessageRequest,
	*mcp.ElicitRequest, error) {

	sm := NewSessionManager(url, apiKey, header, sse)
	defer sm.Close()

	var ret *mcp.CallToolResult
	var th toolHandler
	clntOpts := mcp.ClientOptions{
		CreateMessageHandler: th.samplingHandler,
		ElicitationHandler:   th.elicitationHandler,
	}

	err := sm.WithSession(ctx, mcp.NewClient(&toolImpl, &clntOpts),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			ret, err = sess.CallTool(ctx,
				&mcp.CallToolParams{Name: tool, Arguments: toolArgs})
			return err
		})
	return ret, th.cmr, th.er, err
}

type toolHandler struct {
	cmr *mcp.CreateMessageRequest
	er  *mcp.ElicitRequest
}

func (th *toolHandler) samplingHandler(ctx context.Context,
	req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {

	th.cmr = req

	return &mcp.CreateMessageResult{
		Role:    "assistant",
		Content: &mcp.TextContent{Text: "no response"},
		Model:   "gmcpt-tool",
	}, nil
}

func (th *toolHandler) elicitationHandler(ctx context.Context,
	req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {

	th.er = req

	return &mcp.ElicitResult{
		Action:  "accept",
		Content: defaultElicitContent(req.Params.RequestedSchema),
	}, nil
}

func defaultElicitContent(sch any) map[string]any {
	ret := map[string]any{}
	schema, ok := sch.(map[string]any)
	if !ok {
		return ret
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return ret
	}

	for k, v := range props {
		if prop, ok := v.(map[string]any); ok {
			switch prop["type"] {
			case "string":
				ret[k] = ""
			case "number", "integer":
				ret[k] = 0
			case "boolean":
				ret[k] = false
			default:
				ret[k] = nil
			}
		}
	}

	return ret
}
