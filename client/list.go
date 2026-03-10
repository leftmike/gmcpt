package client

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListOptions uint

const (
	ListPrompts ListOptions = 1 << iota
	ListResources
	ListTools
)

type ListOutput struct {
	Prompts             []*mcp.Prompt   `json:"prompts,omitempty"`
	Resources           []*mcp.Resource `json:"resources,omitempty"`
	Tools               []*mcp.Tool     `json:"tools,omitempty"`
	ToolListChanged     bool            `json:"tool_list_changed,omitempty"`
	PromptListChanged   bool            `json:"prompt_list_changed,omitempty"`
	ResourceListChanged bool            `json:"resource_list_changed,omitempty"`
	ResourceSubscribe   bool            `json:"resource_subscribe,omitempty"`
	Logging             bool            `json:"logging,omitempty"`
	Completions         bool            `json:"completions,omitempty"`
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
		&mcp.CommandTransport{Command: exec.Command(cmd, args...)}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to command: %s", err)
	}
	defer sess.Close()

	return list(ctx, sess, lstOpts)
}

func ListRemote(ctx context.Context, url, apiKey, header string, sse bool,
	lstOpts ListOptions) (*ListOutput, error) {

	sm := NewSessionManager(url, apiKey, header, sse)
	defer sm.Close()

	var lst *ListOutput
	err := sm.WithSession(ctx, mcp.NewClient(&listImpl, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			var err error
			lst, err = list(ctx, sess, lstOpts)
			return err
		})
	return lst, err
}

func list(ctx context.Context, sess *mcp.ClientSession, lstOpts ListOptions) (*ListOutput, error) {
	ir := sess.InitializeResult()
	var lst ListOutput

	if ir.Capabilities.Tools != nil {
		lst.ToolListChanged = ir.Capabilities.Tools.ListChanged
	}
	if ir.Capabilities.Prompts != nil {
		lst.PromptListChanged = ir.Capabilities.Prompts.ListChanged
	}
	if ir.Capabilities.Resources != nil {
		lst.ResourceListChanged = ir.Capabilities.Resources.ListChanged
		lst.ResourceSubscribe = ir.Capabilities.Resources.Subscribe
	}
	if ir.Capabilities.Logging != nil {
		lst.Logging = true
	}
	if ir.Capabilities.Completions != nil {
		lst.Completions = true
	}

	if lstOpts&ListPrompts != 0 && ir.Capabilities.Prompts != nil {
		ret, err := sess.ListPrompts(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing prompts: %s", err)
		}
		lst.Prompts = ret.Prompts
	}

	if lstOpts&ListResources != 0 && ir.Capabilities.Resources != nil {
		ret, err := sess.ListResources(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing resources: %s", err)
		}
		lst.Resources = ret.Resources
	}

	if lstOpts&ListTools != 0 && ir.Capabilities.Tools != nil {
		ret, err := sess.ListTools(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing tools: %s", err)
		}
		lst.Tools = ret.Tools
	}

	return &lst, nil
}
