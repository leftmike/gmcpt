package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

func testListResources(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	resourceURIs []string) {

	lst, err := clnt.ListResources(ctx, mcpgo.ListResourcesRequest{})
	if err != nil {
		t.Errorf("ListResources() failed with %s", err)
	} else if len(lst.Resources) != len(resourceURIs) {
		t.Errorf("ListResources() got %d want %d", len(lst.Resources), len(resourceURIs))
	} else {
		found := map[string]struct{}{}
		for _, r := range lst.Resources {
			if !slices.Contains(resourceURIs, r.URI) {
				t.Errorf("ListResources() unexpected resource: %s", r.URI)
			} else {
				found[r.URI] = struct{}{}
			}
		}
		for _, uri := range resourceURIs {
			if _, ok := found[uri]; !ok {
				t.Errorf("ListResources() missing %s resource", uri)
			}
		}
	}
}

func testReadResource(t *testing.T, ctx context.Context, clnt *mcpclnt.Client, uri string,
	expected string) {

	ret, err := clnt.ReadResource(ctx, mcpgo.ReadResourceRequest{
		Request: mcpgo.Request{Method: "resources/read"},
		Params: mcpgo.ReadResourceParams{
			URI: uri,
		},
	})
	if err != nil {
		t.Errorf("ReadResource(%s) failed with %s", uri, err)
	} else if len(ret.Contents) == 0 {
		t.Errorf("ReadResource(%s) missing contents", uri)
	} else {
		tc, ok := mcpgo.AsTextResourceContents(ret.Contents[0])
		if !ok {
			t.Errorf("ReadResource(%s) expected TextResourceContents, got %T: %#v", uri,
				ret.Contents[0], ret.Contents[0])
		} else {
			if tc.Text != expected {
				t.Errorf("ReadResource(%s) got %q want %q", uri, tc.Text, expected)
			}
		}
	}
}

func testResources(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	testListResources(t, ctx, clnt, []string{"file:///config.json", "file:///readme.txt"})
	testReadResource(t, ctx, clnt, "file:///config.json", `{"version": "1.0", "debug": true}`)
	testReadResource(t, ctx, clnt, "file:///readme.txt", "Welcome to the project!")
}

func TestProxyResourcesSSE(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testResources)
}

func TestProxyResourcesHTTP(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testResources)
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

func testResourcesChanged(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer) {

	onNotify := make(chan string, 4)
	clnt.OnNotification(func(notify mcpgo.JSONRPCNotification) {
		onNotify <- notify.Method
	})

	testListResources(t, ctx, clnt, []string{"file:///config.json", "file:///readme.txt"})

	tsvr.AddResource(
		mcpgo.NewResource("file:///notes.txt", "notes.txt",
			mcpgo.WithResourceDescription("Project notes")),
		func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents,
			error) {

			return []mcpgo.ResourceContents{
				mcpgo.TextResourceContents{
					URI:  "file:///notes.txt",
					Text: "These are important notes.",
				},
			}, nil
		})

	timeout := 2 * time.Second
	select {
	case method := <-onNotify:
		if method != "notifications/resources/list_changed" {
			t.Errorf("OnNotification() got %s want notifications/resources/list_changed", method)
		}
	case <-time.After(timeout):
		t.Errorf("OnNotification() timed out after %v", timeout)
	}

	testListResources(t, ctx, clnt,
		[]string{"file:///config.json", "file:///readme.txt", "file:///notes.txt"})
	testReadResource(t, ctx, clnt, "file:///config.json", `{"version": "1.0", "debug": true}`)
	testReadResource(t, ctx, clnt, "file:///readme.txt", "Welcome to the project!")
	testReadResource(t, ctx, clnt, "file:///notes.txt", "These are important notes.")
}

func TestProxyResourcesChangedSSE(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testResourcesChanged)
}

func TestProxyResourcesChangedHTTP(t *testing.T) {
	tsvr := newResourcesMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testResourcesChanged)
}

func TestWithSessionRetrySuccess(t *testing.T) {
	var failed, success bool

	start := time.Now()
	handler := mcpsvr.NewStreamableHTTPServer(newToolsMCPServer())
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if time.Since(start) < 500*time.Millisecond {
			failed = true
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		} else {
			handler.ServeHTTP(w, r)
		}
	}))
	defer svr.Close()

	prx := NewProxy(svr.URL+"/mcp", "", "", false)
	prx.retry = true

	err := prx.withSession(context.Background(),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			success = true
			return nil
		})
	if err != nil {
		t.Errorf("withSession() failed with %s", err)
	} else {
		if !failed {
			t.Error("server never failed")
		}
		if !success {
			t.Error("withSession() session never established")
		}
		if prx.sess == nil {
			t.Error("withSession() prx.sess == nil")
		}

		prx.sess.Close()
	}
}

func TestWithSessionRetryContextCancel(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer svr.Close()

	prx := NewProxy(svr.URL+"/mcp", "", "", false)
	prx.retry = true

	ctx, _ := context.WithTimeout(context.Background(), 500*time.Millisecond)
	err := prx.withSession(ctx,
		func(ctx context.Context, sess *mcp.ClientSession) error {
			t.Error("withSession() should not call with")
			return nil
		})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("withSession() got %s want %s", err, context.DeadlineExceeded)
	}
}

func requireAPIKey(handler http.Handler, headerName, apiKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerName) != apiKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func TestProxyAPIKey(t *testing.T) {
	cases := []struct {
		h    string
		key  string
		pkey string
		ph   string
		fail bool
	}{
		{h: "X-API-Key", key: "123abc", ph: "X-API-Key", pkey: "123abc"},
		{
			h: "Authorization", key: "Bearer token-456",
			ph: "Authorization", pkey: "Bearer token-456",
		},
		{h: "X-API-Key", key: "123abc", fail: true},
		{h: "X-API-Key", key: "correct-key", pkey: "wrong-key", ph: "X-API-Key", fail: true},
	}

	configs := []struct {
		nhfn func(tsvr *mcpsvr.MCPServer) http.Handler
		ep   string
		sse  bool
	}{
		{
			nhfn: func(tsvr *mcpsvr.MCPServer) http.Handler {
				return mcpsvr.NewSSEServer(tsvr)
			},
			ep:  "/sse",
			sse: true,
		},
		{
			nhfn: func(tsvr *mcpsvr.MCPServer) http.Handler {
				return mcpsvr.NewStreamableHTTPServer(tsvr)
			},
			ep: "/mcp",
		},
	}

	for _, c := range cases {
		for _, cfg := range configs {
			tsvr := newToolsMCPServer()
			svr := httptest.NewServer(requireAPIKey(cfg.nhfn(tsvr), c.h, c.key))
			prx := NewProxy(svr.URL+cfg.ep, c.pkey, c.ph, cfg.sse)

			if c.fail {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				err := prx.run(ctx, slog.Default(), &mcp.IOTransport{Reader: nil, Writer: nil})
				if err == nil {
					t.Errorf("Proxy.run(%s, %v) did not fail", cfg.ep, c)
					prx.Close()
				}
			} else {
				testProxy(t, prx, tsvr, testTools)
			}
		}
	}
}
