package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestMCPServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-upstream-server", "0.1.0", mcpsvr.WithToolCapabilities(true))

	echoTool := mcpgo.NewTool("echo",
		mcpgo.WithDescription("Echoes back the input"),
		mcpgo.WithString("message",
			mcpgo.Required(),
			mcpgo.Description("Message to echo"),
		),
	)

	tsvr.AddTool(echoTool,
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			msg := req.GetString("message", "")
			return mcpgo.NewToolResultText(fmt.Sprintf("Echo: %s", msg)), nil
		})

	return tsvr
}

// type testProxyFunc func(t *testing.T, ctx context.Context) //, sess XXX, tsvr *mcpsvr.MCPServer)

func testProxy(t *testing.T, prx *Proxy, tsvr *mcpsvr.MCPServer) { //, testFunc testProxyFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create pipes for the proxy's stdio transport
	clientReader, proxyWriter := io.Pipe()
	proxyReader, clientWriter := io.Pipe()

	// Create IOTransport for the proxy (reads from proxyReader, writes to proxyWriter)
	proxyTransport := &mcp.IOTransport{
		Reader: proxyReader,
		Writer: proxyWriter,
	}

	// Run the proxy in a goroutine
	go func() {
		prx.run(ctx, slog.Default(), proxyTransport)
	}()

	// Create IOTransport for the client (reads from clientReader, writes to clientWriter)
	clientTransport := &mcp.IOTransport{
		Reader: clientReader,
		Writer: clientWriter,
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "0.1.0"},
		nil,
	)

	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Failed to connect client to proxy: %v", err)
	}
	defer sess.Close()

	t.Log("Client connected to proxy successfully")

	// List tools through the proxy
	toolsResult, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	if len(toolsResult.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(toolsResult.Tools))
	}

	if toolsResult.Tools[0].Name != "echo" {
		t.Fatalf("Expected tool 'echo', got '%s'", toolsResult.Tools[0].Name)
	}

	t.Logf("Found tool: %s", toolsResult.Tools[0].Name)

	// Call the echo tool through the proxy
	callResult, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello world"},
	})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	if len(callResult.Content) == 0 {
		t.Fatal("Expected content in tool result")
	}

	textContent, ok := callResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", callResult.Content[0])
	}

	expected := "Echo: hello world"
	if textContent.Text != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, textContent.Text)
	}

	t.Logf("Tool call result: %s", textContent.Text)

	// Clean up: close proxy's upstream connection before closing pipes
	prx.Close()

	cancel()
	clientReader.Close()
	clientWriter.Close()
	proxyReader.Close()
	proxyWriter.Close()
}

func TestProxySSE(t *testing.T) {
	tsvr := newTestMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr)
}

func TestProxyStreamableHTTP(t *testing.T) {
	tsvr := newTestMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr)
}
