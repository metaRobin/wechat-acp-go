package session

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metaRobin/wechat-router-go/internal/agent"
)

// mockBackend is a test Backend that echoes input with a prefix.
type mockBackend struct {
	mu       sync.Mutex
	prompts  []string
	killed   bool
	response string
	delay    time.Duration
}

func (m *mockBackend) Prompt(_ context.Context, text string, onText func(chunk string)) (*agent.PromptResult, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, text)
	resp := m.response
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if resp == "" {
		resp = "echo: " + text
	}
	if onText != nil {
		onText(resp)
	}
	return &agent.PromptResult{Text: resp, StopReason: "end"}, nil
}

func (m *mockBackend) Kill() {
	m.mu.Lock()
	m.killed = true
	m.mu.Unlock()
}

func (m *mockBackend) getPrompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.prompts))
	copy(out, m.prompts)
	return out
}

func mockRegistry(mock *mockBackend) BackendRegistry {
	return BackendRegistry{
		"test": func(ctx context.Context) (agent.Backend, error) {
			return mock, nil
		},
	}
}

func TestIntegration_EnqueueAndReply(t *testing.T) {
	mock := &mockBackend{}

	var repliesMu sync.Mutex
	var replies []struct{ key, token, text string }

	mgr := NewManager(ManagerOpts{
		Registry:      mockRegistry(mock),
		DefaultAgent:  "test",
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		OnReply: func(sessionKey, contextToken, text string) {
			repliesMu.Lock()
			replies = append(replies, struct{ key, token, text string }{sessionKey, contextToken, text})
			repliesMu.Unlock()
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	err := mgr.Enqueue("u:user1", PendingMessage{
		Text:         "hello",
		ContextToken: "user1",
	}, "test")
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	prompts := mock.getPrompts()
	if len(prompts) != 1 || prompts[0] != "hello" {
		t.Errorf("prompts = %v, want [hello]", prompts)
	}

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(replies))
	}
	if replies[0].key != "u:user1" {
		t.Errorf("reply key = %q, want u:user1", replies[0].key)
	}
	if replies[0].text != "echo: hello" {
		t.Errorf("reply text = %q, want echo: hello", replies[0].text)
	}
}

func TestIntegration_MultipleMessages(t *testing.T) {
	mock := &mockBackend{}

	var repliesMu sync.Mutex
	var replies []string

	mgr := NewManager(ManagerOpts{
		Registry:      mockRegistry(mock),
		DefaultAgent:  "test",
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		OnReply: func(_, _, text string) {
			repliesMu.Lock()
			replies = append(replies, text)
			repliesMu.Unlock()
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	for _, msg := range []string{"msg1", "msg2", "msg3"} {
		if err := mgr.Enqueue("u:user1", PendingMessage{Text: msg, ContextToken: "user1"}, "test"); err != nil {
			t.Fatalf("Enqueue %s failed: %v", msg, err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	prompts := mock.getPrompts()
	if len(prompts) != 3 {
		t.Fatalf("prompts count = %d, want 3", len(prompts))
	}

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) != 3 {
		t.Fatalf("replies count = %d, want 3", len(replies))
	}
	for i, want := range []string{"echo: msg1", "echo: msg2", "echo: msg3"} {
		if replies[i] != want {
			t.Errorf("replies[%d] = %q, want %q", i, replies[i], want)
		}
	}
}

func TestIntegration_MultipleSessions(t *testing.T) {
	var repliesMu sync.Mutex
	var replies []struct{ key, text string }

	registry := BackendRegistry{
		"test": func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{}, nil
		},
	}

	mgr := NewManager(ManagerOpts{
		Registry:      registry,
		DefaultAgent:  "test",
		IdleTimeout:   time.Hour,
		MaxConcurrent: 10,
		OnReply: func(sessionKey, _, text string) {
			repliesMu.Lock()
			replies = append(replies, struct{ key, text string }{sessionKey, text})
			repliesMu.Unlock()
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	_ = mgr.Enqueue("u:alice", PendingMessage{Text: "hi from alice", ContextToken: "alice"}, "test")
	_ = mgr.Enqueue("u:bob", PendingMessage{Text: "hi from bob", ContextToken: "bob"}, "test")

	time.Sleep(500 * time.Millisecond)

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) != 2 {
		t.Fatalf("replies count = %d, want 2", len(replies))
	}

	found := map[string]bool{}
	for _, r := range replies {
		found[r.key] = true
	}
	if !found["u:alice"] || !found["u:bob"] {
		t.Errorf("replies = %v, want both u:alice and u:bob", replies)
	}
}

func TestIntegration_WithStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	mock := &mockBackend{}

	var replied atomic.Bool
	mgr := NewManager(ManagerOpts{
		Registry:      mockRegistry(mock),
		DefaultAgent:  "test",
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		Store:         store,
		OnReply: func(_, _, _ string) {
			replied.Store(true)
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "persist me", ContextToken: "user1"}, "test")
	time.Sleep(300 * time.Millisecond)

	if !replied.Load() {
		t.Fatal("expected reply callback")
	}

	sess, err := store.GetSession("u:user1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess == nil {
		t.Fatal("session not persisted to store")
	}
	if sess.State != StateActive {
		t.Errorf("session state = %q, want active", sess.State)
	}
	if sess.AgentID != "test" {
		t.Errorf("session agent_id = %q, want test", sess.AgentID)
	}

	msgs, err := store.LoadRecentMessages("u:user1", 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages count = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "persist me" {
		t.Errorf("msgs[0] = %+v, want user/persist me", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "echo: persist me" {
		t.Errorf("msgs[1] = %+v, want assistant/echo: persist me", msgs[1])
	}
}

func TestIntegration_SwitchAgent(t *testing.T) {
	var repliesMu sync.Mutex
	var replies []string

	registry := BackendRegistry{
		"agentA": func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{response: "from A"}, nil
		},
		"agentB": func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{response: "from B"}, nil
		},
	}

	mgr := NewManager(ManagerOpts{
		Registry:      registry,
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		OnReply: func(_, _, text string) {
			repliesMu.Lock()
			replies = append(replies, text)
			repliesMu.Unlock()
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	// Use agent A
	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "hi", ContextToken: "user1"}, "agentA")
	time.Sleep(200 * time.Millisecond)

	if mgr.GetSessionAgentID("u:user1") != "agentA" {
		t.Errorf("agent = %q, want agentA", mgr.GetSessionAgentID("u:user1"))
	}

	// Switch to agent B
	if err := mgr.SwitchAgent("u:user1", "agentB"); err != nil {
		t.Fatalf("SwitchAgent: %v", err)
	}

	if mgr.GetSessionAgentID("u:user1") != "agentB" {
		t.Errorf("agent after switch = %q, want agentB", mgr.GetSessionAgentID("u:user1"))
	}

	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "hi again", ContextToken: "user1"}, "agentB")
	time.Sleep(200 * time.Millisecond)

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) < 2 {
		t.Fatalf("replies count = %d, want >= 2", len(replies))
	}
	if replies[0] != "from A" {
		t.Errorf("replies[0] = %q, want from A", replies[0])
	}
	if replies[len(replies)-1] != "from B" {
		t.Errorf("replies[last] = %q, want from B", replies[len(replies)-1])
	}
}

func TestIntegration_MaxConcurrentEviction(t *testing.T) {
	var repliesMu sync.Mutex
	replyCount := 0

	registry := BackendRegistry{
		"test": func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{delay: 10 * time.Millisecond}, nil
		},
	}

	mgr := NewManager(ManagerOpts{
		Registry:      registry,
		IdleTimeout:   time.Hour,
		MaxConcurrent: 2,
		OnReply: func(_, _, _ string) {
			repliesMu.Lock()
			replyCount++
			repliesMu.Unlock()
		},
		Logger: slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "a", ContextToken: "u1"}, "test")
	_ = mgr.Enqueue("u:user2", PendingMessage{Text: "b", ContextToken: "u2"}, "test")
	time.Sleep(200 * time.Millisecond)

	_ = mgr.Enqueue("u:user3", PendingMessage{Text: "c", ContextToken: "u3"}, "test")
	time.Sleep(200 * time.Millisecond)

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if replyCount != 3 {
		t.Errorf("replyCount = %d, want 3", replyCount)
	}
}

func TestIntegration_NoAgentSelected(t *testing.T) {
	registry := BackendRegistry{
		"test": func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{}, nil
		},
	}

	mgr := NewManager(ManagerOpts{
		Registry:      registry,
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		Logger:        slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	err := mgr.Enqueue("u:user1", PendingMessage{Text: "hello", ContextToken: "user1"}, "")
	if err == nil {
		t.Fatal("expected error for empty agent ID, got nil")
	}
}

func TestFormatHistory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	_ = store.UpsertSession("u:user1", StateActive, "test")
	_ = store.AppendMessage("u:user1", "user", "hello")
	_ = store.AppendMessage("u:user1", "assistant", "hi there")
	_ = store.AppendMessage("u:user1", "user", "how are you")

	mgr := NewManager(ManagerOpts{
		Registry:     mockRegistry(&mockBackend{}),
		HistoryLimit: 20,
		Store:        store,
		Logger:       slog.Default(),
	})

	history := mgr.formatHistory("u:user1")
	if history == "" {
		t.Fatal("expected non-empty history")
	}
	if !strings.Contains(history, "[以下是之前的对话历史]") {
		t.Error("missing history header")
	}
	if !strings.Contains(history, "User: hello") {
		t.Error("missing user message")
	}
	if !strings.Contains(history, "Assistant: hi there") {
		t.Error("missing assistant message")
	}
	if !strings.Contains(history, "[对话历史结束]") {
		t.Error("missing history footer")
	}
}

func TestFormatHistory_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	mgr := NewManager(ManagerOpts{
		Registry: mockRegistry(&mockBackend{}),
		Store:    store,
		Logger:   slog.Default(),
	})

	history := mgr.formatHistory("u:nobody")
	if history != "" {
		t.Errorf("expected empty history, got %q", history)
	}
}

func TestIntegration_HistoryInjection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	// Pre-populate history
	_ = store.UpsertSession("u:user1", StateSuspended, "test")
	_ = store.AppendMessage("u:user1", "user", "previous question")
	_ = store.AppendMessage("u:user1", "assistant", "previous answer")

	mock := &mockBackend{}

	mgr := NewManager(ManagerOpts{
		Registry:      mockRegistry(mock),
		DefaultAgent:  "test",
		HistoryLimit:  20,
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		Store:         store,
		OnReply:       func(_, _, _ string) {},
		Logger:        slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	// First message should inject history
	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "new question", ContextToken: "user1"}, "test")
	time.Sleep(300 * time.Millisecond)

	prompts := mock.getPrompts()
	if len(prompts) < 1 {
		t.Fatal("expected at least 1 prompt")
	}
	if !strings.Contains(prompts[0], "[以下是之前的对话历史]") {
		t.Errorf("first prompt should contain history, got: %s", prompts[0][:min(len(prompts[0]), 100)])
	}
	if !strings.Contains(prompts[0], "new question") {
		t.Error("first prompt should contain the actual message")
	}

	// Second message should NOT inject history
	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "follow up", ContextToken: "user1"}, "test")
	time.Sleep(300 * time.Millisecond)

	prompts = mock.getPrompts()
	if len(prompts) < 2 {
		t.Fatal("expected at least 2 prompts")
	}
	if strings.Contains(prompts[1], "[以下是之前的对话历史]") {
		t.Error("second prompt should NOT contain history")
	}
	if prompts[1] != "follow up" {
		t.Errorf("second prompt = %q, want 'follow up'", prompts[1])
	}
}

func TestFormatHistory_Limit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	_ = store.UpsertSession("u:user1", StateActive, "test")
	for i := 0; i < 50; i++ {
		_ = store.AppendMessage("u:user1", "user", fmt.Sprintf("msg%d", i))
	}

	mgr := NewManager(ManagerOpts{
		Registry:     mockRegistry(&mockBackend{}),
		HistoryLimit: 5,
		Store:        store,
		Logger:       slog.Default(),
	})

	history := mgr.formatHistory("u:user1")
	// Should only contain the 5 most recent messages (msg45-msg49)
	if !strings.Contains(history, "msg49") {
		t.Error("should contain most recent message")
	}
	if strings.Contains(history, "msg0") {
		t.Error("should NOT contain oldest message with limit=5")
	}
}

func TestIntegration_ClearCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	mock := &mockBackend{}

	mgr := NewManager(ManagerOpts{
		Registry:      mockRegistry(mock),
		DefaultAgent:  "test",
		IdleTimeout:   time.Hour,
		MaxConcurrent: 5,
		Store:         store,
		OnReply:       func(_, _, _ string) {},
		Logger:        slog.Default(),
	})
	mgr.Start()
	defer mgr.Stop()

	// Create a session with messages
	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "hello", ContextToken: "user1"}, "test")
	time.Sleep(200 * time.Millisecond)

	// Verify messages exist
	msgs, _ := store.LoadRecentMessages("u:user1", 10)
	if len(msgs) == 0 {
		t.Fatal("expected messages to exist before clear")
	}

	// Clear
	mgr.RemoveSession("u:user1")

	// Verify messages deleted
	msgs, _ = store.LoadRecentMessages("u:user1", 10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs))
	}

	// Verify session removed
	if mgr.HasSession("u:user1") {
		t.Error("session should be removed after clear")
	}
}
