package client

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListOptions uint

const (
	ListOptionsTools ListOptions = 1 << iota
	ListOptionsPrompts
	ListOptionsResources
	ListOptionsAll = ListOptionsTools | ListOptionsPrompts | ListOptionsResources
)

type ListOutput struct {
	Tools     []*mcp.Tool     `json:"tools,omitempty"`
	Prompts   []*mcp.Prompt   `json:"prompts,omitempty"`
	Resources []*mcp.Resource `json:"resources,omitempty"`
}

var (
	listImpl = mcp.Implementation{
		Name:    "gmcpt-list-client",
		Version: "0.1.0",
	}
)

func ListLocal(ctx context.Context, cmd string, args []string, lstOpts ListOptions) (*ListOutput,
	error) {

	sess, err := mcp.NewClient(&listImpl, nil).Connect(ctx,
		&mcp.CommandTransport{
			Command: exec.Command(cmd, args...),
		}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	return list(ctx, sess, lstOpts)
}

func ListRemote(ctx context.Context, url, apiKey, header string, sse bool,
	lstOpts ListOptions) (*ListOutput, error) {

	sm := NewSessionManager(url, apiKey, header, sse)

	var lst *ListOutput
	err := sm.WithSession(ctx,
		mcp.NewClient(&listImpl, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			lst, err = list(ctx, sess, lstOpts)
			return err
		})
	return lst, err
}

func list(ctx context.Context, sess *mcp.ClientSession, lstOpts ListOptions) (*ListOutput, error) {
	if lstOpts == 0 {
		lstOpts = ListOptionsAll
	}

	ir := sess.InitializeResult()
	var lst ListOutput

	if lstOpts&ListOptionsTools != 0 && ir.Capabilities.Tools != nil {
		ret, err := sess.ListTools(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing tools: %s", err)
		}
		lst.Tools = ret.Tools
	}

	if lstOpts&ListOptionsPrompts != 0 && ir.Capabilities.Prompts != nil {
		ret, err := sess.ListPrompts(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing prompts: %s", err)
		}
		lst.Prompts = ret.Prompts
	}

	if lstOpts&ListOptionsResources != 0 && ir.Capabilities.Resources != nil {
		ret, err := sess.ListResources(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing resources: %s", err)
		}
		lst.Resources = ret.Resources
	}

	return &lst, nil
}
