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

	clnt         *mcp.Client
	sess         *mcp.ClientSession
	ir           *mcp.InitializeResult
	retry        bool
	toolNames    map[string]struct{}
	promptNames  map[string]struct{}
	resourceURIs map[string]struct{}
	svr          *mcp.Server
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
			ToolListChangedHandler:     prx.toolListChanged,
			PromptListChangedHandler:   prx.promptListChanged,
			ResourceListChangedHandler: prx.resourceListChanged,
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

			slog.Info("with session", "backoff", backoff, "error", err.Error())

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

func (prx *Proxy) Close() {
	if prx.sess != nil {
		prx.sess.Close()
		prx.sess = nil
	}
	for sess := range prx.svr.Sessions() {
		sess.Close()
	}
}

func (prx *Proxy) toolListChanged(ctx context.Context, req *mcp.ToolListChangedRequest) {
	slog.Info("tool list changed")

	err := prx.withSession(ctx, prx.updateTools)
	if err != nil {
		slog.Error("update tools", "error", err.Error())
		panic(fmt.Sprintf("unabled to update tools after tool list changed notification: %s", err))
	}
}

func (prx *Proxy) updateTools(ctx context.Context, sess *mcp.ClientSession) error {
	ret, err := sess.ListTools(ctx, nil)
	if err != nil {
		slog.Error("list tools", "error", err.Error())
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
						slog.Error("json unmarshal", "name", name, "args", req.Params.Arguments,
							"error", err.Error())
						return err
					}
				}

				slog.Info("call tool", "name", name, "args", args)

				var err error
				ret, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					slog.Error("call tool", "name", name, "args", args, "error", err)
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

func (prx *Proxy) promptListChanged(ctx context.Context, req *mcp.PromptListChangedRequest) {
	slog.Info("prompt list changed")

	err := prx.withSession(ctx, prx.updatePrompts)
	if err != nil {
		slog.Error("update prompts", "error", err)
		panic(fmt.Sprintf("unabled to update prompts after prompt list changed notification: %s",
			err))
	}
}

func (prx *Proxy) updatePrompts(ctx context.Context, sess *mcp.ClientSession) error {
	ret, err := sess.ListPrompts(ctx, nil)
	if err != nil {
		slog.Error("list prompts", "error", err)
		return err
	}

	newNames := map[string]struct{}{}
	for _, pr := range ret.Prompts {
		newNames[pr.Name] = struct{}{}
	}

	var remove []string
	for name := range prx.promptNames {
		if _, ok := newNames[name]; !ok {
			remove = append(remove, name)
		}
	}
	if len(remove) > 0 {
		prx.svr.RemovePrompts(remove...)
	}

	for _, pr := range ret.Prompts {
		if _, ok := prx.promptNames[pr.Name]; ok {
			continue
		}
		prx.svr.AddPrompt(pr, prx.promptHandler(pr.Name))
	}

	prx.promptNames = newNames
	return nil
}

func (prx *Proxy) promptHandler(name string) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var ret *mcp.GetPromptResult
		err := prx.withSession(ctx,
			func(ctx context.Context, sess *mcp.ClientSession) error {
				var err error
				ret, err = sess.GetPrompt(ctx, &mcp.GetPromptParams{
					Name:      name,
					Arguments: req.Params.Arguments,
				})
				if err != nil {
					slog.Error("get prompt", "name", name, "args", req.Params.Arguments,
						"error", err)
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

func (prx *Proxy) resourceListChanged(ctx context.Context, req *mcp.ResourceListChangedRequest) {
	slog.Info("resource list changed")

	err := prx.withSession(ctx, prx.updateResources)
	if err != nil {
		slog.Error("update resources", "error", err)
		panic(fmt.Sprintf(
			"unabled to update resources after resource list changed notification: %s", err))
	}
}

func (prx *Proxy) updateResources(ctx context.Context, sess *mcp.ClientSession) error {
	ret, err := sess.ListResources(ctx, nil)
	if err != nil {
		slog.Error("list resources", "error", err)
		return err
	}

	newURIs := map[string]struct{}{}
	for _, rs := range ret.Resources {
		newURIs[rs.URI] = struct{}{}
	}

	var remove []string
	for uri := range prx.resourceURIs {
		if _, ok := newURIs[uri]; !ok {
			remove = append(remove, uri)
		}
	}
	if len(remove) > 0 {
		prx.svr.RemoveResources(remove...)
	}

	for _, rs := range ret.Resources {
		if _, ok := prx.resourceURIs[rs.URI]; ok {
			continue
		}
		prx.svr.AddResource(rs, prx.resourceHandler(rs.URI))
	}

	prx.resourceURIs = newURIs
	return nil
}

func (prx *Proxy) resourceHandler(uri string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var ret *mcp.ReadResourceResult
		err := prx.withSession(ctx,
			func(ctx context.Context, sess *mcp.ClientSession) error {
				var err error
				ret, err = sess.ReadResource(ctx, &mcp.ReadResourceParams{
					URI: uri,
				})
				if err != nil {
					slog.Error("read resource", "uri", uri, "error", err)
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

	slog.Info("initialize result", "capabilities", ir.Capabilities,
		"instructions", ir.Instructions, "protocol_version", ir.ProtocolVersion,
		"server_name", ir.ServerInfo.Name, "server_title", ir.ServerInfo.Title,
		"server_version", ir.ServerInfo.Version, "server_website", ir.ServerInfo.WebsiteURL)

	prx.ir = ir
	prx.retry = true
	return nil
}

func (prx *Proxy) Run(ctx context.Context, l *slog.Logger, logProto string) error {
	t := mcp.Transport(&mcp.StdioTransport{})

	if logProto != "" {
		file, err := os.OpenFile(logProto, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			t = &mcp.LoggingTransport{
				Transport: t,
				Writer:    file,
			}
		} else {
			slog.Error("open file", "logproto", logProto, "error", err)
		}
	}

	return prx.run(ctx, l, t)
}

func (prx *Proxy) run(ctx context.Context, l *slog.Logger, t mcp.Transport) error {
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

	if prx.ir.Capabilities.Resources != nil {
		err := prx.withSession(ctx, prx.updateResources)
		if err != nil {
			return err
		}
	}

	if prx.ir.Capabilities.Tools != nil {
		err := prx.withSession(ctx, prx.updateTools)
		if err != nil {
			return err
		}
	}

	if prx.ir.Capabilities.Prompts != nil {
		err := prx.withSession(ctx, prx.updatePrompts)
		if err != nil {
			return err
		}
	}

	return prx.svr.Run(ctx, t)
}
