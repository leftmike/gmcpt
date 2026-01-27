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

func newTestMCPServer() *mcpsvr.MCPServer {
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

type testProxyFunc func(t *testing.T, ctx context.Context, clnt *mcpclnt.Client,
	tsvr *mcpsvr.MCPServer)

func testProxy(t *testing.T, prx *Proxy, tsvr *mcpsvr.MCPServer, testFunc testProxyFunc) {
	// Test Client <-> Proxy <-> Test Server

	// Test Client -> Proxy
	prxReader, clntWriter := io.Pipe()
	// Test Client <- Proxy
	clntReader, prxWriter := io.Pipe()

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

	// XXX: use defer?
	prx.Close()
	clntWriter.Close()
	prxWriter.Close()
	clntReader.Close()
	prxReader.Close()
	cancel()
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
				t.Fatalf("CallTool(echo) expected %s got %s", expected, tc.Text)
			}
		}
	}
}

func TestProxySSE(t *testing.T) {
	tsvr := newTestMCPServer()
	svr := httptest.NewServer(mcpsvr.NewSSEServer(tsvr))
	defer svr.Close()

	fmt.Println("sse server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/sse", "", "", true), tsvr, testToolCall)
}

func TestProxyStreamableHTTP(t *testing.T) {
	tsvr := newTestMCPServer()
	svr := mcpsvr.NewTestStreamableHTTPServer(tsvr)
	defer svr.Close()

	fmt.Println("streamable http server url:", svr.URL)

	testProxy(t, NewProxy(svr.URL+"/mcp", "", "", false), tsvr, testToolCall)
}
