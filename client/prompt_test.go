package client

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func promptMessageToString(msg *mcp.PromptMessage) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "    Role: %s\n", msg.Role)
	switch cnt := msg.Content.(type) {
	case *mcp.TextContent:
		if len(cnt.Text) > 200 {
			fmt.Fprintf(&buf, "    Text: %s... (%d bytes)\n", cnt.Text[:200], len(cnt.Text))
		} else {
			fmt.Fprintf(&buf, "    Text: %s\n", cnt.Text)
		}
	default:
		fmt.Fprintf(&buf, "    Content: %T\n", msg.Content)
	}
	return buf.String()
}

func promptMessagesToString(msgs []*mcp.PromptMessage) string {
	var buf strings.Builder
	for i, msg := range msgs {
		fmt.Fprintf(&buf, "  [%d]:\n%s", i, promptMessageToString(msg))
	}
	return buf.String()
}

func TestGetPromptRemoteServers(t *testing.T) {
	cases := []struct {
		short      bool
		url        string
		sse        bool
		prompt     string
		promptArgs map[string]string
		desc       string
		role       mcp.Role
		text       string
		contains   string
	}{
		{
			short:    true,
			url:      "https://docs.mcp.cloudflare.com/sse",
			sse:      true,
			prompt:   "workers-prompt-full",
			role:     "user",
			contains: "<system_context>",
		},
		{
			short:      true,
			url:        "https://hf.co/mcp",
			prompt:     "Model Details",
			promptArgs: map[string]string{"model_id": "openai-community/gpt2"},
			desc:       "Model details for openai-community/gpt2",
			role:       "user",
			contains:   "# openai-community/gpt2",
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "Paper Summary",
			promptArgs: map[string]string{"paper_id": "2502.16161"},
			desc:       "Paper summary for 2502.16161",
			role:       "user",
			text:       paperSummary2502_16161,
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "User Summary",
			promptArgs: map[string]string{"user_id": "clem"},
			desc:       "User summary for clem",
			role:       "user",
			contains:   "# clem",
		},
		{
			url:        "https://hf.co/mcp",
			prompt:     "Dataset Details",
			promptArgs: map[string]string{"dataset_id": "squad"},
			desc:       "Dataset details for squad",
			role:       "user",
			contains:   "# rajpurkar/squad",
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
		} else if len(ret.Messages) == 0 {
			t.Errorf("GetPromptRemote(%s, %s) returned no messages", c.url, c.prompt)
		} else {
			if ret.Description != c.desc {
				t.Errorf("GetPromptRemote(%s, %s) description got %s want %s", c.url, c.prompt,
					ret.Description, c.desc)
			}
			msg := ret.Messages[0]
			if msg.Role != c.role {
				t.Errorf("GetPromptRemote(%s, %s) role got %s want %s", c.url, c.prompt,
					msg.Role, c.role)
			}
			tc, ok := msg.Content.(*mcp.TextContent)
			if !ok {
				t.Errorf("GetPromptRemote(%s, %s) content type got %T want *mcp.TextContent",
					c.url, c.prompt, msg.Content)
			} else {
				if c.text != "" {
					if tc.Text != c.text {
						t.Errorf("GetPromptRemote(%s, %s) text got\n%s\nwant\n%s", c.url, c.prompt,
							tc.Text, c.text)
					}
				} else if c.contains != "" {
					if !strings.Contains(tc.Text, c.contains) {
						t.Errorf("GetPromptRemote(%s, %s) text does not contain %s\n%s",
							c.url, c.prompt, c.contains, promptMessagesToString(ret.Messages))
					}
				}
			}
		}
	}
}

var (
	paperSummary2502_16161 = `# OmniParser V2: Structured-Points-of-Thought for Unified Visual Text   Parsing and Its Generality to Multimodal Large Language Models

**Authors:** Wenwen Yu, Zhibo Yang, Jianqiang Wan, Sibo Song, Jun Tang, Wenqing Cheng, Yuliang Liu, Xiang Bai
**Published:** 22 Feb, 2025
**Upvotes:** 1

**Links:**
- [Hugging Face Paper Page](https://hf.co/papers/2502.16161)
- [arXiv Page](https://arxiv.org/abs/2502.16161)

## Abstract

Visually-situated text parsing (VsTP) has recently seen notable advancements,
driven by the growing demand for automated document understanding and the
emergence of large language models capable of processing document-based
questions. While various methods have been proposed to tackle the complexities
of VsTP, existing solutions often rely on task-specific architectures and
objectives for individual tasks. This leads to modal isolation and complex
workflows due to the diversified targets and heterogeneous schemas. In this
paper, we introduce OmniParser V2, a universal model that unifies VsTP typical
tasks, including text spotting, key information extraction, table recognition,
and layout analysis, into a unified framework. Central to our approach is the
proposed Structured-Points-of-Thought (SPOT) prompting schemas, which improves
model performance across diverse scenarios by leveraging a unified
encoder-decoder architecture, objective, and input\&output representation. SPOT
eliminates the need for task-specific architectures and loss functions,
significantly simplifying the processing pipeline. Our extensive evaluations
across four tasks on eight different datasets show that OmniParser V2 achieves
state-of-the-art or competitive results in VsTP. Additionally, we explore the
integration of SPOT within a multimodal large language model structure, further
enhancing text localization and recognition capabilities, thereby confirming
the generality of SPOT prompting technique. The code is available at
https://github.com/AlibabaResearch/AdvancedLiterateMachinery{AdvancedLiterateMachinery}.


**Note:** Tags and paper references on Hugging Face are not always complete or up-to-date. -- validate information if necessary


Please provide a summary of this paper and any associated resources.`
)
