package client

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newResourceServer() *mcpsvr.MCPServer {
	tsvr := mcpsvr.NewMCPServer("test-resource-server", "0.1.0",
		mcpsvr.WithResourceCapabilities(false, false))

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

func resourceContentsToString(rcnts []*mcp.ResourceContents) string {
	var buf strings.Builder
	fmt.Fprintln(&buf, "[]*mcp.ResourceContents{")
	for _, rc := range rcnts {
		fmt.Fprintln(&buf, "    {")
		if rc.URI != "" {
			fmt.Fprintf(&buf, "        URI: %q,\n", rc.URI)
		}
		if rc.MIMEType != "" {
			fmt.Fprintf(&buf, "        MIMEType: %q,\n", rc.MIMEType)
		}
		if rc.Text != "" {
			fmt.Fprintf(&buf, "        Text: `%s`,\n", rc.Text)
		}
		if rc.Blob != nil {
			fmt.Fprintf(&buf, "        Blob: %#v,\n", rc.Blob)
		}
		if rc.Meta != nil {
			fmt.Fprintf(&buf, "        Meta: %#v,\n", rc.Meta)
		}
		fmt.Fprintln(&buf, "    },")
	}
	fmt.Fprintln(&buf, "}")

	return buf.String()
}

func TestReadResourceRemoteServers(t *testing.T) {
	cases := []struct {
		short bool
		url   string
		sse   bool
		uri   string
		rcnts []*mcp.ResourceContents
	}{
		{
			short: true,
			url:   "https://mcp.exa.ai/mcp",
			uri:   "exa://tools/list",
			rcnts: toolsListContents,
		},
	}

	for _, c := range cases {
		if testing.Short() && !c.short {
			fmt.Printf("skipping %s resource %s\n", c.url, c.uri)
			continue
		}

		ret, err := ReadResourceRemote(context.Background(), c.url, "", "", c.sse, c.uri)
		if err != nil {
			t.Errorf("ReadResourceRemote(%s, %s) failed: %s", c.url, c.uri, err)
		} else {
			if !reflect.DeepEqual(ret.Contents, c.rcnts) {
				t.Errorf("ReadResourceRemote(%s, %s) got\n%swant\n%s", c.url, c.uri,
					resourceContentsToString(ret.Contents), resourceContentsToString(c.rcnts))
			}
		}
	}
}

var (
	toolsListContents = []*mcp.ResourceContents{
		{
			URI:      "exa://tools/list",
			MIMEType: "application/json",
			Text: `[
  {
    "id": "web_search_exa",
    "name": "Web Search (Exa)",
    "description": "Real-time web search using Exa AI",
    "enabled": true
  },
  {
    "id": "web_search_advanced_exa",
    "name": "Advanced Web Search (Exa)",
    "description": "Advanced web search with full Exa API control including category filters, domain restrictions, date ranges, highlights, summaries, and subpage crawling",
    "enabled": false
  },
  {
    "id": "get_code_context_exa",
    "name": "Code Context Search",
    "description": "Search for code snippets, examples, and documentation from open source repositories",
    "enabled": true
  },
  {
    "id": "company_research_exa",
    "name": "Company Research",
    "description": "Research companies and organizations",
    "enabled": true
  },
  {
    "id": "crawling_exa",
    "name": "Web Crawling",
    "description": "Extract content from specific URLs",
    "enabled": false
  },
  {
    "id": "deep_researcher_start",
    "name": "Deep Researcher Start",
    "description": "Start a comprehensive AI research task",
    "enabled": false
  },
  {
    "id": "deep_researcher_check",
    "name": "Deep Researcher Check",
    "description": "Check status and retrieve results of research task",
    "enabled": false
  },
  {
    "id": "people_search_exa",
    "name": "People Search",
    "description": "Search for people and professional profiles",
    "enabled": false
  },
  {
    "id": "linkedin_search_exa",
    "name": "LinkedIn Search (Deprecated)",
    "description": "Deprecated: Use people_search_exa instead",
    "enabled": false
  }
]`,
		},
	}
)
