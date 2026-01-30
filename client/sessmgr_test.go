package client

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsvr "github.com/mark3labs/mcp-go/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() {
	slog.SetLogLoggerLevel(slog.LevelError)
}

func TestWithSessionRetrySuccess(t *testing.T) {
	var failed, success bool

	start := time.Now()
	handler := mcpsvr.NewStreamableHTTPServer(mcpsvr.NewMCPServer("test-server", "0.1.0"))
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if time.Since(start) < 500*time.Millisecond {
			failed = true
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		} else {
			handler.ServeHTTP(w, r)
		}
	}))
	defer svr.Close()

	sm := NewSessionManager(svr.URL+"/mcp", "", "", false)
	sm.retry = true

	err := sm.WithSession(context.Background(),
		mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			success = true
			return nil
		})
	if err != nil {
		t.Errorf("WithSession() failed with %s", err)
	} else {
		if !failed {
			t.Error("server never failed")
		}
		if !success {
			t.Error("WithSession() session never established")
		}

		sm.Close()
	}
}

func TestWithSessionRetryContextCancel(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer svr.Close()

	sm := NewSessionManager(svr.URL+"/mcp", "", "", false)
	sm.retry = true

	ctx, _ := context.WithTimeout(context.Background(), 500*time.Millisecond)
	err := sm.WithSession(ctx,
		mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil),
		func(ctx context.Context, sess *mcp.ClientSession) error {
			t.Error("WithSession() should not call with")
			return nil
		})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("WithSession() got %s want %s", err, context.DeadlineExceeded)
	}
}
