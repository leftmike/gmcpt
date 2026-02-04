package client

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
)

func newResourceServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-resource-server", "0.1.0",
		mcpsvr.WithResourceCapabilities(false, false),
	)

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

func testReadResource(t *testing.T, uri string, expected string) {
	svr := mcpsvr.NewTestStreamableHTTPServer(newResourceServer())
	defer svr.Close()

	ret, err := ReadResourceRemote(context.Background(), svr.URL+"/mcp", "", "", false, uri)
	if err != nil {
		t.Errorf("ReadResourceRemote(%s) failed: %s", uri, err)
	} else if len(ret.Contents) == 0 {
		t.Errorf("ReadResourceRemote(%s) returned no contents", uri)
	} else if ret.Contents[0].Text != expected {
		t.Errorf("ReadResourceRemote(%s) text got %s want %s", uri, ret.Contents[0].Text, expected)
	}
}

func TestReadResource(t *testing.T) {
	testReadResource(t, "file:///config.json", `{"version": "1.0", "debug": true}`)
	testReadResource(t, "file:///readme.txt", "Welcome to the project!")
}
