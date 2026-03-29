package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/metaRobin/wechat-router-go/internal/agent"
	"github.com/metaRobin/wechat-router-go/internal/session"
)

// noopBackend is a minimal agent.Backend that does nothing.
type noopBackend struct{}

func (n *noopBackend) Prompt(_ context.Context, _ string, _ func(string)) (*agent.PromptResult, error) {
	return &agent.PromptResult{Text: "ok", StopReason: "end"}, nil
}
func (n *noopBackend) Kill() {}

func newTestManager(t *testing.T) *session.Manager {
	t.Helper()
	registry := session.BackendRegistry{
		"test": func(_ context.Context) (agent.Backend, error) { return &noopBackend{}, nil },
	}
	mgr := session.NewManager(session.ManagerOpts{
		Registry:      registry,
		DefaultAgent:  "test",
		MaxConcurrent: 10,
		OnReply:       func(_, _, _ string) {},
		Logger:        slog.Default(),
	})
	mgr.Start()
	t.Cleanup(mgr.Stop)
	return mgr
}

func newTestServer(t *testing.T, mgr *session.Manager, store *session.Store) *Server {
	t.Helper()
	return New(mgr, store, "127.0.0.1:0", slog.Default())
}

func TestHandleSessions_ListEmpty(t *testing.T) {
	mgr := newTestManager(t)
	srv := newTestServer(t, mgr, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var result []SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d sessions", len(result))
	}
}

func TestHandleSessions_GetDetailNotFound(t *testing.T) {
	mgr := newTestManager(t)
	srv := newTestServer(t, mgr, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?key=u:nobody", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleSessions_GetDetailFromStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := session.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	_ = store.UpsertSession("u:bob", session.StateActive, "claude")
	_ = store.AppendMessage("u:bob", "user", "hello")
	_ = store.AppendMessage("u:bob", "assistant", "hi there")

	mgr := newTestManager(t)
	srv := newTestServer(t, mgr, store)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?key=u:bob", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var detail SessionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Key != "u:bob" {
		t.Errorf("key = %q, want u:bob", detail.Key)
	}
	if len(detail.Messages) != 2 {
		t.Errorf("messages count = %d, want 2", len(detail.Messages))
	}
	if detail.Messages[0].Role != "user" {
		t.Errorf("first message role = %q, want user", detail.Messages[0].Role)
	}
}

func newTestManagerWithStore(t *testing.T, store *session.Store) *session.Manager {
	t.Helper()
	registry := session.BackendRegistry{
		"test": func(_ context.Context) (agent.Backend, error) { return &noopBackend{}, nil },
	}
	mgr := session.NewManager(session.ManagerOpts{
		Registry:      registry,
		DefaultAgent:  "test",
		MaxConcurrent: 10,
		OnReply:       func(_, _, _ string) {},
		Store:         store,
		Logger:        slog.Default(),
	})
	mgr.Start()
	t.Cleanup(mgr.Stop)
	return mgr
}

func TestHandleSessions_Delete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := session.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	_ = store.UpsertSession("u:carol", session.StateActive, "claude")
	_ = store.AppendMessage("u:carol", "user", "test message")

	mgr := newTestManagerWithStore(t, store)
	srv := newTestServer(t, mgr, store)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions?key=u:carol", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	msgs, _ := store.LoadRecentMessages("u:carol", 10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after delete, got %d", len(msgs))
	}
}

func TestHandleSessions_DeleteMissingKey(t *testing.T) {
	mgr := newTestManager(t)
	srv := newTestServer(t, mgr, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleSessions_MethodNotAllowed(t *testing.T) {
	mgr := newTestManager(t)
	srv := newTestServer(t, mgr, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
