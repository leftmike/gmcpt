package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
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

func newToolsMCPServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-upstream-server", "0.1.0", mcpsvr.WithToolCapabilities(true))

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

func testListTools(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, toolNames []string) {
	lst, err := clnt.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		t.Errorf("ListTools() failed with %s", err)
	} else if len(lst.Tools) != len(toolNames) {
		t.Errorf("ListTools() got %d want %d", len(lst.Tools), len(toolNames))
	} else {
		found := map[string]struct{}{}
		for _, tool := range lst.Tools {
			if !slices.Contains(toolNames, tool.Name) {
				t.Errorf("ListTools() unexpected tool: %s", tool.Name)
			} else {
				found[tool.Name] = struct{}{}
			}
		}
		for _, name := range toolNames {
			if _, ok := found[name]; !ok {
				t.Errorf("ListTools() missing %s tool", name)
			}
		}
	}
}

func testToolCall(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, name string,
	args map[string]any, expected string) {

	ret, err := clnt.CallTool(ctx, mcpgo.CallToolRequest{
		Request: mcpgo.Request{Method: "tools/call"},
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	})
	if err != nil {
		t.Errorf("CallTool(%s) failed with %s", name, err)
	} else if len(ret.Content) == 0 {
		t.Errorf("CallTool(%s) missing result content", name)
	} else {
		tc, ok := ret.Content[0].(mcpgo.TextContent)
		if !ok {
			t.Errorf("CallTool(%s) expected TextContent, got %T: %#v", name, ret.Content[0],
				ret.Content[0])
		} else {
			if tc.Text != expected {
				t.Errorf("CallTool(%s) got %s want %s", name, tc.Text, expected)
			}
		}
	}
}

func testTools(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, tsvr *mcpsvr.MCPServer) {
	testListTools(t, ctx, clnt, []string{"echo", "add"})
	testToolCall(t, ctx, clnt, "echo", map[string]any{"message": "hello world"},
		"echo: hello world")
	testToolCall(t, ctx, clnt, "add", map[string]any{"a": 3.5, "b": 2.5}, "sum: 6")
}

func TestProxyToolSSE(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testTools)
}

func TestProxyToolHTTP(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testTools)
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

func testListPrompts(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	promptNames []string) {

	lst, err := clnt.ListPrompts(ctx, mcpgo.ListPromptsRequest{})
	if err != nil {
		t.Errorf("ListPrompts() failed with %s", err)
	} else if len(lst.Prompts) != len(promptNames) {
		t.Errorf("ListPrompts() got %d want %d", len(lst.Prompts), len(promptNames))
	} else {
		found := map[string]struct{}{}
		for _, p := range lst.Prompts {
			if !slices.Contains(promptNames, p.Name) {
				t.Errorf("ListPrompts() unexpected prompt: %s", p.Name)
			} else {
				found[p.Name] = struct{}{}
			}
		}
		for _, name := range promptNames {
			if _, ok := found[name]; !ok {
				t.Errorf("ListPrompts() missing %s prompt", name)
			}
		}
	}
}

func testGetPrompt(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, name string,
	args map[string]string, expected string, role mcpgo.Role) {

	ret, err := clnt.GetPrompt(ctx, mcpgo.GetPromptRequest{
		Request: mcpgo.Request{Method: "prompts/get"},
		Params: mcpgo.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	})
	if err != nil {
		t.Errorf("GetPrompt(%s) failed with %s", name, err)
	} else if len(ret.Messages) == 0 {
		t.Errorf("GetPrompt(%s) missing messages", name)
	} else {
		tc, ok := ret.Messages[0].Content.(mcpgo.TextContent)
		if !ok {
			t.Errorf("GetPrompt(%s) expected TextContent, got %T: %#v", name,
				ret.Messages[0].Content, ret.Messages[0].Content)
		} else {
			if tc.Text != expected {
				t.Errorf("GetPrompt(%s) got %q want %q", name, tc.Text, expected)
			}
			if ret.Messages[0].Role != role {
				t.Errorf("GetPrompt(%s) role got %q want %q", name, ret.Messages[0].Role, role)
			}
		}
	}
}

func testPrompts(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, tsvr *mcpsvr.MCPServer) {
	testListPrompts(t, ctx, clnt, []string{"greet", "help"})
	testGetPrompt(t, ctx, clnt, "greet", map[string]string{"name": "World"}, "Hello, World!",
		mcpgo.RoleUser)
	testGetPrompt(t, ctx, clnt, "help", nil, "This is the help message.", mcpgo.RoleAssistant)
}

func TestProxyPromptsSSE(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testPrompts)
}

func TestProxyPromptsHTTP(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testPrompts)
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

func testToolsChanged(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	onNotify := make(chan string, 4)
	clnt.OnNotification(func(notify mcpgo.JSONRPCNotification) {
		onNotify <- notify.Method
	})

	testListTools(t, ctx, clnt, []string{"echo", "add"})

	tsvr.AddTool(mcpgo.NewTool("multiply",
		mcpgo.WithDescription("multiplies two numbers"),
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
			return mcpgo.NewToolResultText(fmt.Sprintf("product: %g", a*b)), nil
		})

	timeout := 2 * time.Second
	select {
	case method := <-onNotify:
		if method != "notifications/tools/list_changed" {
			t.Errorf("OnNotification() got %s want notifications/tools/list_changed", method)
		}
	case <-time.After(timeout):
		t.Errorf("OnNotification() timed out after %v", timeout)
	}

	testListTools(t, ctx, clnt, []string{"echo", "add", "multiply"})
	testToolCall(t, ctx, clnt, "echo", map[string]any{"message": "hello world"},
		"echo: hello world")
	testToolCall(t, ctx, clnt, "add", map[string]any{"a": 3.5, "b": 2.5}, "sum: 6")
	testToolCall(t, ctx, clnt, "multiply", map[string]any{"a": 3.0, "b": 4.0}, "product: 12")
}

func TestProxyToolsChangedSSE(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testToolsChanged)
}

func TestProxyToolsChangedHTTP(t *testing.T) {
	tsvr := newToolsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testToolsChanged)
}

func testPromptsChanged(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	onNotify := make(chan string, 4)
	clnt.OnNotification(func(notify mcpgo.JSONRPCNotification) {
		onNotify <- notify.Method
	})

	testListPrompts(t, ctx, clnt, []string{"greet", "help"})

	tsvr.AddPrompt(mcpgo.NewPrompt("farewell",
		mcpgo.WithPromptDescription("generates a farewell message"),
		mcpgo.WithArgument("name",
			mcpgo.ArgumentDescription("name to bid farewell"),
			mcpgo.RequiredArgument(),
		)),
		func(ctx context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
			name := req.Params.Arguments["name"]
			return mcpgo.NewGetPromptResult(
				"a farewell message",
				[]mcpgo.PromptMessage{
					mcpgo.NewPromptMessage(mcpgo.RoleUser,
						mcpgo.NewTextContent(fmt.Sprintf("Goodbye, %s!", name))),
				},
			), nil
		})

	timeout := 2 * time.Second
	select {
	case method := <-onNotify:
		if method != "notifications/prompts/list_changed" {
			t.Errorf("OnNotification() got %s want notifications/prompts/list_changed", method)
		}
	case <-time.After(timeout):
		t.Errorf("OnNotification() timed out after %v", timeout)
	}

	testListPrompts(t, ctx, clnt, []string{"greet", "help", "farewell"})
	testGetPrompt(t, ctx, clnt, "greet", map[string]string{"name": "Dog"}, "Hello, Dog!",
		mcpgo.RoleUser)
	testGetPrompt(t, ctx, clnt, "help", nil, "This is the help message.", mcpgo.RoleAssistant)
	testGetPrompt(t, ctx, clnt, "farewell", map[string]string{"name": "World"}, "Goodbye, World!",
		mcpgo.RoleUser)
}

func TestProxyPromptsChangedSSE(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testPromptsChanged)
}

func TestProxyPromptsChangedHTTP(t *testing.T) {
	tsvr := newPromptsMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testPromptsChanged)
}
