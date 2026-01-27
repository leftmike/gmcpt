package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	mcpclnt "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() {
	slog.SetLogLoggerLevel(slog.LevelError)
}

func newToolsMCPServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-upstream-server", "0.1.0", mcpsvr.WithToolCapabilities(true))

	echoTool := mcpgo.NewTool("echo",
		mcpgo.WithDescription("echoes back the input"),
		mcpgo.WithString("message",
			mcpgo.Required(),
			mcpgo.Description("message to echo"),
		),
	)

	tsvr.AddTool(echoTool,
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			msg := req.GetString("message", "")
			return mcpgo.NewToolResultText(fmt.Sprintf("echo: %s", msg)), nil
		})

	return tsvr
}

func newPromptsMCPServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-upstream-server", "0.1.0",
		mcpsvr.WithPromptCapabilities(true))

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

type testProxyFunc func(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer)

func testProxy(t *testing.T, prx *Proxy, tsvr *mcpsvr.MCPServer, testFunc testProxyFunc) {
	// Test Client <-> Proxy <-> Test Server

	// Test Client -> Proxy
	prxReader, clntWriter := io.Pipe()
	// Test Client <- Proxy
	clntReader, prxWriter := io.Pipe()
	defer func() {
		prxReader.Close()
		clntWriter.Close()
		clntReader.Close()
		prxWriter.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go func() {
		err := prx.run(ctx, slog.Default(), &mcp.IOTransport{Reader: prxReader, Writer: prxWriter})
		if err != nil && ctx.Err() == nil {
			t.Fatalf("proxy.run() failed with %s", err)
		}
	}()

	clnt := mcpclnt.NewClient(mcptransport.NewIO(clntReader, clntWriter, nil))
	err := clnt.Start(ctx)
	if err != nil {
		t.Fatalf("client.NewClient() failed with %s", err)
	}
	defer clnt.Close()

	_, err = clnt.Initialize(ctx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcpgo.Implementation{
				Name:    "test-client",
				Version: "0.1.0",
			},
		},
	})
	if err != nil {
		t.Fatalf("client.Initialize() failed with %s", err)
	}

	testFunc(t, ctx, clnt, tsvr)

	prx.Close()
}

func testToolCall(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	lst, err := clnt.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		t.Errorf("ListTools() failed with %s", err)
	} else if len(lst.Tools) != 1 {
		t.Errorf("ListTools() got %d want 1", len(lst.Tools))
	} else if lst.Tools[0].Name != "echo" {
		t.Errorf("ListTools() got %s want echo", lst.Tools[0].Name)
	}

	ret, err := clnt.CallTool(ctx, mcpgo.CallToolRequest{
		Request: mcpgo.Request{Method: "tools/call"},
		Params: mcpgo.CallToolParams{
			Name:      "echo",
			Arguments: map[string]any{"message": "hello world"},
		},
	})
	if err != nil {
		t.Errorf("CallTool(echo) failed with %s", err)
	} else if len(ret.Content) == 0 {
		t.Errorf("CallTool(echo) missing result content")
	} else {
		tc, ok := ret.Content[0].(mcpgo.TextContent)
		if !ok {
			t.Fatalf("CallTool(echo) expected TextContent, got %T: %#v", ret.Content[0],
				ret.Content[0])
		} else {
			expected := "echo: hello world"
			if tc.Text != expected {
				t.Fatalf("CallTool(echo) got %s want %s", tc.Text, expected)
			}
		}
	}
}

func TestProxyToolSSE(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testToolCall)
}

func TestProxyToolHTTP(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testToolCall)
}

func testPromptList(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	lst, err := clnt.ListPrompts(ctx, mcpgo.ListPromptsRequest{})
	if err != nil {
		t.Errorf("ListPrompts() failed with %s", err)
	} else if len(lst.Prompts) != 2 {
		t.Errorf("ListPrompts() got %d want 2", len(lst.Prompts))
	} else {
		var greet, help bool
		for _, p := range lst.Prompts {
			switch p.Name {
			case "greet":
				greet = true
			case "help":
				help = true
			default:
				t.Errorf("ListPrompts() unexpected prompt: %s", p.Name)
			}
		}
		if !greet {
			t.Errorf("ListPrompts() missing greet prompt")
		}
		if !help {
			t.Errorf("ListPrompts() missing help prompt")
		}
	}
}

func testPromptGet(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	ret, err := clnt.GetPrompt(ctx, mcpgo.GetPromptRequest{
		Request: mcpgo.Request{Method: "prompts/get"},
		Params: mcpgo.GetPromptParams{
			Name:      "greet",
			Arguments: map[string]string{"name": "World"},
		},
	})
	if err != nil {
		t.Errorf("GetPrompt(greet) failed with %s", err)
	} else if len(ret.Messages) == 0 {
		t.Errorf("GetPrompt(greet) missing messages")
	} else {
		tc, ok := ret.Messages[0].Content.(mcpgo.TextContent)
		if !ok {
			t.Errorf("GetPrompt(greet) expected TextContent, got %T: %#v",
				ret.Messages[0].Content, ret.Messages[0].Content)
		} else {
			expected := "Hello, World!"
			if tc.Text != expected {
				t.Errorf("GetPrompt(greet) got %q want %q", tc.Text, expected)
			}
			if ret.Messages[0].Role != mcpgo.RoleUser {
				t.Errorf("GetPrompt(greet) role got %q want %q", ret.Messages[0].Role,
					mcpgo.RoleUser)
			}
		}
	}

	ret, err = clnt.GetPrompt(ctx, mcpgo.GetPromptRequest{
		Request: mcpgo.Request{Method: "prompts/get"},
		Params: mcpgo.GetPromptParams{
			Name: "help",
		},
	})
	if err != nil {
		t.Errorf("GetPrompt(help) failed with %s", err)
	} else if len(ret.Messages) == 0 {
		t.Errorf("GetPrompt(help) missing messages")
	} else {
		tc, ok := ret.Messages[0].Content.(mcpgo.TextContent)
		if !ok {
			t.Errorf("GetPrompt(help) expected TextContent, got %T: %#v",
				ret.Messages[0].Content, ret.Messages[0].Content)
		} else {
			expected := "This is the help message."
			if tc.Text != expected {
				t.Errorf("GetPrompt(help) got %q want %q", tc.Text, expected)
			}
			if ret.Messages[0].Role != mcpgo.RoleAssistant {
				t.Errorf("GetPrompt(help) role got %q want %q", ret.Messages[0].Role,
					mcpgo.RoleAssistant)
			}
		}
	}
}

func TestProxyPromptsSSE(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testPromptList)
	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testPromptGet)
}

func TestProxyPromptsHTTP(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testPromptList)
	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testPromptGet)
}

func newResourcesMCPServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-upstream-server", "0.1.0",
		mcpsvr.WithResourceCapabilities(false, true))

	tsvr.AddResource(
		mcpgo.NewResource("file:///config.json", "config.json",
			mcpgo.WithResourceDescription("Application configuration file"),
			mcpgo.WithMIMEType("application/json")),
		func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents,
			error) {

			return []mcpgo.ResourceContents{
				mcpgo.TextResourceContents{
					URI:      "file:///config.json",
					MIMEType: "application/json",
					Text:     `{"version": "1.0", "debug": true}`,
				},
			}, nil
		})

	tsvr.AddResource(
		mcpgo.NewResource("file:///readme.txt", "readme.txt",
			mcpgo.WithResourceDescription("Project readme")),
		func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents,
			error) {

			return []mcpgo.ResourceContents{
				mcpgo.TextResourceContents{
					URI:  "file:///readme.txt",
					Text: "Welcome to the project!",
				},
			}, nil
		})

	return tsvr
}

func testResourceList(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	lst, err := clnt.ListResources(ctx, mcpgo.ListResourcesRequest{})
	if err != nil {
		t.Errorf("ListResources() failed with %s", err)
	} else if len(lst.Resources) != 2 {
		t.Errorf("ListResources() got %d want 2", len(lst.Resources))
	} else {
		var config, readme bool
		for _, r := range lst.Resources {
			switch r.URI {
			case "file:///config.json":
				config = true
				if r.Name != "config.json" {
					t.Errorf("ListResources() config.json name got %s want config.json", r.Name)
				}
			case "file:///readme.txt":
				readme = true
				if r.Name != "readme.txt" {
					t.Errorf("ListResources() readme.txt name got %q want readme.txt", r.Name)
				}
			default:
				t.Errorf("ListResources() unexpected resource: %s", r.URI)
			}
		}
		if !config {
			t.Errorf("ListResources() missing config.json resource")
		}
		if !readme {
			t.Errorf("ListResources() missing readme.txt resource")
		}
	}
}

func testResourceRead(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	ret, err := clnt.ReadResource(ctx, mcpgo.ReadResourceRequest{
		Request: mcpgo.Request{Method: "resources/read"},
		Params: mcpgo.ReadResourceParams{
			URI: "file:///config.json",
		},
	})
	if err != nil {
		t.Errorf("ReadResource(config.json) failed with %s", err)
	} else if len(ret.Contents) == 0 {
		t.Errorf("ReadResource(config.json) missing contents")
	} else {
		tc, ok := mcpgo.AsTextResourceContents(ret.Contents[0])
		if !ok {
			t.Errorf("ReadResource(config.json) expected TextResourceContents, got %T: %#v",
				ret.Contents[0], ret.Contents[0])
		} else {
			expected := `{"version": "1.0", "debug": true}`
			if tc.Text != expected {
				t.Errorf("ReadResource(config.json) got %q want %q", tc.Text, expected)
			}
			if tc.URI != "file:///config.json" {
				t.Errorf("ReadResource(config.json) URI got %q want %q",
					tc.URI, "file:///config.json")
			}
		}
	}

	ret, err = clnt.ReadResource(ctx, mcpgo.ReadResourceRequest{
		Request: mcpgo.Request{Method: "resources/read"},
		Params: mcpgo.ReadResourceParams{
			URI: "file:///readme.txt",
		},
	})
	if err != nil {
		t.Errorf("ReadResource(readme.txt) failed with %s", err)
	} else if len(ret.Contents) == 0 {
		t.Errorf("ReadResource(readme.txt) missing contents")
	} else {
		tc, ok := mcpgo.AsTextResourceContents(ret.Contents[0])
		if !ok {
			t.Errorf("ReadResource(readme.txt) expected TextResourceContents, got %T: %#v",
				ret.Contents[0], ret.Contents[0])
		} else {
			expected := "Welcome to the project!"
			if tc.Text != expected {
				t.Errorf("ReadResource(readme.txt) got %q want %q", tc.Text, expected)
			}
		}
	}
}

func TestProxyResourcesSSE(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testResourceList)
	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testResourceRead)
}

func TestProxyResourcesHTTP(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testResourceList)
	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testResourceRead)
}
