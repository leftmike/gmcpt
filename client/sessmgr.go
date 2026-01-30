package client

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SessionManager struct {
	url    string
	apiKey string
	header string
	sse    bool

	sess  *mcp.ClientSession
	Retry bool
}

func NewSessionManager(url, apiKey, header string, sse bool) SessionManager {
	return SessionManager{
		url:    url,
		apiKey: apiKey,
		header: header,
		sse:    sse,
	}
}

func (sm *SessionManager) transport() mcp.Transport {
	if sm.sse {
		return &mcp.SSEClientTransport{
			Endpoint:   sm.url,
			HTTPClient: sm.httpClient(),
		}
	}

	return &mcp.StreamableClientTransport{
		Endpoint:   sm.url,
		HTTPClient: sm.httpClient(),
	}
}

func (sm *SessionManager) WithSession(ctx context.Context, clnt *mcp.Client,
	with func(ctx context.Context, sess *mcp.ClientSession) error) error {

	if sm.sess != nil && sm.sess.Ping(ctx, nil) != nil {
		sm.sess.Close()
		sm.sess = nil
	}

	if sm.sess == nil {
		backoff := 250 * time.Millisecond
		for {
			var err error
			sm.sess, err = clnt.Connect(ctx, sm.transport(), nil)
			if err == nil {
				break
			} else if !sm.Retry {
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

	return with(ctx, sm.sess)
}

func (sm *SessionManager) httpClient() *http.Client {
	if sm.apiKey != "" {
		return &http.Client{Transport: sm}
	}

	return http.DefaultClient
}

func (sm *SessionManager) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(sm.header, sm.apiKey)
	return http.DefaultTransport.RoundTrip(req)
}

func (sm *SessionManager) Close() {
	if sm.sess != nil {
		sm.sess.Close()
		sm.sess = nil
	}
}
