/*
To Do:
- Connect to the server before the client to find out which capabilities to advertise

- If the connection is closed by the server, calls or notifications will return an error wrapping ErrConnectionClosed.
*/

package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Proxy struct {
	url    string
	apiKey string
	header string
	sse    bool

	clnt      *mcp.Client
	sess      *mcp.ClientSession
	ir        *mcp.InitializeResult
	retry     bool
	toolNames map[string]struct{}
	svr       *mcp.Server
}

func NewProxy(url, apiKey, header string, sse bool) *Proxy {
	prx := &Proxy{
		url:    url,
		apiKey: apiKey,
		header: header,
		sse:    sse,
	}

	prx.clnt = mcp.NewClient(
		&mcp.Implementation{Name: "gmcpt-proxy-client", Version: "0.1.0"},
		&mcp.ClientOptions{
			// CreateMessageHandler
			// ElicitationHandler
			// Capabilities
			// ElicitationCompleteHandler
			// ElicitationCompleteHandler
			ToolListChangedHandler: prx.toolListChanged,
			// PromptListChangedHandler
			// ResourceListChangedHandler
			// ResourceUpdatedHandler
			// LoggingMessageHandler
			// ProgressNotificationHandler
		})

	return prx
}

func (prx *Proxy) transport() mcp.Transport {
	if prx.sse {
		return &mcp.SSEClientTransport{
			Endpoint:   prx.url,
			HTTPClient: prx.httpClient(),
		}
	}

	return &mcp.StreamableClientTransport{
		Endpoint:   prx.url,
		HTTPClient: prx.httpClient(),
	}
}

func (prx *Proxy) withSession(ctx context.Context,
	with func(ctx context.Context, sess *mcp.ClientSession) error) error {

	if prx.sess != nil && prx.sess.Ping(ctx, nil) != nil {
		prx.sess.Close()
		prx.sess = nil
	}

	if prx.sess == nil {
		backoff := time.Second
		for {
			var err error
			prx.sess, err = prx.clnt.Connect(ctx, prx.transport(), nil)
			if err == nil {
				break
			} else if !prx.retry {
				return err
			}

			slog.Info("with session", slog.Duration("backoff", backoff),
				slog.String("error", err.Error()))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff = min(backoff*2, 30*time.Second)
			}
		}
	}

	return with(ctx, prx.sess)
}

func (prx *Proxy) httpClient() *http.Client {
	if prx.apiKey != "" {
		return &http.Client{Transport: prx}
	}

	return http.DefaultClient
}

func (prx *Proxy) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	// if t.headerName == "Authorization" {
	//	r.Header.Set(t.headerName, "Bearer "+t.apiKey)
	req.Header.Set(prx.header, prx.apiKey)
	return http.DefaultTransport.RoundTrip(req)
}

func (prx *Proxy) toolListChanged(ctx context.Context, req *mcp.ToolListChangedRequest) {
	slog.Info("tool list changed")

	err := prx.withSession(ctx, prx.updateTools)
	if err != nil {
		slog.Error("update tools", slog.String("error", err.Error()))
		panic(fmt.Sprintf("unabled to update tools after tool list changed notification: %s", err))
	}
}

func (prx *Proxy) updateTools(ctx context.Context, sess *mcp.ClientSession) error {
	ret, err := sess.ListTools(ctx, nil)
	if err != nil {
		slog.Error("list tools", slog.String("error", err.Error()))
		return err
	}

	newNames := map[string]struct{}{}
	for _, tl := range ret.Tools {
		newNames[tl.Name] = struct{}{}
	}

	var remove []string
	for name := range prx.toolNames {
		if _, ok := newNames[name]; !ok {
			remove = append(remove, name)
		}
	}
	if len(remove) > 0 {
		prx.svr.RemoveTools(remove...)
	}

	for _, tl := range ret.Tools {
		if _, ok := prx.toolNames[tl.Name]; ok {
			continue
		}
		prx.svr.AddTool(tl, prx.toolHandler(tl.Name))
	}

	prx.toolNames = newNames
	return nil
}

func (prx *Proxy) toolHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var ret *mcp.CallToolResult
		err := prx.withSession(ctx,
			func(ctx context.Context, sess *mcp.ClientSession) error {
				var args map[string]any
				if len(req.Params.Arguments) > 0 {
					err := json.Unmarshal(req.Params.Arguments, &args)
					if err != nil {
						slog.Error("json unmarshal", slog.String("name", name),
							slog.Any("args", req.Params.Arguments),
							slog.String("error", err.Error()))
						return err
					}
				}

				var err error
				ret, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					slog.Error("call tool", slog.String("name", name), slog.Any("args", args),
						slog.String("error", err.Error()))
					return err
				}
				return nil
			})

		if err != nil {
			return nil, err
		}
		return ret, nil
	}
}

func (prx *Proxy) initializeResult(ctx context.Context, sess *mcp.ClientSession) error {
	ir := sess.InitializeResult()

	slog.Info("initialize result", slog.Any("capabilities", ir.Capabilities),
		slog.String("instructions", ir.Instructions),
		slog.String("protocol_version", ir.ProtocolVersion),
		slog.String("server_name", ir.ServerInfo.Name),
		slog.String("server_title", ir.ServerInfo.Title),
		slog.String("server_version", ir.ServerInfo.Version),
		slog.String("server_website", ir.ServerInfo.WebsiteURL))

	prx.ir = ir
	prx.retry = true
	return nil
}

func setupTransport(logProto string, t mcp.Transport) mcp.Transport {
	if logProto != "" {
		file, err := os.OpenFile(logProto, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("open file", slog.String("logproto", logProto),
				slog.String("error", err.Error()))
			return t
		}

		return &mcp.LoggingTransport{
			Transport: t,
			Writer:    file,
		}
	}

	return t
}

func (prx *Proxy) Run(ctx context.Context, l *slog.Logger, logProto string) error {
	err := prx.withSession(ctx, prx.initializeResult)
	if err != nil {
		return err
	}

	prx.svr = mcp.NewServer(
		&mcp.Implementation{Name: "gmcpt-proxy-server", Version: "0.1.0"},
		&mcp.ServerOptions{
			Logger: l,
		})

	// ir.Capabilities.Completions
	// ir.Capabilities.Logging
	// ir.Capabilities.Prompts
	// ir.Capabilities.Resources
	if prx.ir.Capabilities.Tools != nil {
		// prx.toolNames = map[string]struct{}{}
		err := prx.withSession(ctx, prx.updateTools)
		if err != nil {
			return err
		}
	}

	return prx.svr.Run(ctx, setupTransport(logProto, &mcp.StdioTransport{}))
}

/*
func (p *Proxy) registerResources(ctx context.Context) error {
	session, err := p.getSession(ctx)
	if err != nil {
		return err
	}

	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		return err
	}

	for _, res := range resources.Resources {
		r := res
		p.server.AddResource(&r, p.makeResourceHandler(r.URI))
	}
	log.Printf("Registered %d resources", len(resources.Resources))
	return nil
}

func (p *Proxy) makeResourceHandler(uri string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		session, err := p.getSession(ctx)
		if err != nil {
			return nil, err
		}

		result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			if reconnErr := p.reconnect(ctx); reconnErr != nil {
				return nil, fmt.Errorf("read failed and reconnect failed: %w", err)
			}
			session, _ = p.getSession(ctx)
			result, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		}
		return result, err
	}
}

func (p *Proxy) registerPrompts(ctx context.Context) error {
	session, err := p.getSession(ctx)
	if err != nil {
		return err
	}

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		return err
	}

	for _, prompt := range prompts.Prompts {
		pr := prompt
		p.server.AddPrompt(&pr, p.makePromptHandler(pr.Name))
	}
	log.Printf("Registered %d prompts", len(prompts.Prompts))
	return nil
}

func (p *Proxy) makePromptHandler(name string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		session, err := p.getSession(ctx)
		if err != nil {
			return nil, err
		}

		result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: req.Params.Arguments})
		if err != nil {
			if reconnErr := p.reconnect(ctx); reconnErr != nil {
				return nil, fmt.Errorf("get prompt failed and reconnect failed: %w", err)
			}
			session, _ = p.getSession(ctx)
			result, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: req.Params.Arguments})
		}
		return result, err
	}
}
*/
