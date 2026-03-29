package session

import (
	"context"
	"log/slog"
	"path/filepath"
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
	response string // configurable response; defaults to echo
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

// TestIntegration_EnqueueAndReply verifies the full message flow:
// Enqueue → session created → Backend.Prompt called → OnReply callback fires.
func TestIntegration_EnqueueAndReply(t *testing.T) {
	mock := &mockBackend{}

	var repliesMu sync.Mutex
	var replies []struct{ key, token, text string }

	mgr := NewManager(ManagerOpts{
		NewBackend: func(ctx context.Context) (agent.Backend, error) {
			return mock, nil
		},
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

	// Send a message
	err := mgr.Enqueue("u:user1", PendingMessage{
		Text:         "hello",
		ContextToken: "user1",
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify backend received the prompt
	prompts := mock.getPrompts()
	if len(prompts) != 1 || prompts[0] != "hello" {
		t.Errorf("prompts = %v, want [hello]", prompts)
	}

	// Verify reply callback was called
	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(replies))
	}
	if replies[0].key != "u:user1" {
		t.Errorf("reply key = %q, want u:user1", replies[0].key)
	}
	if replies[0].token != "user1" {
		t.Errorf("reply token = %q, want user1", replies[0].token)
	}
	if replies[0].text != "echo: hello" {
		t.Errorf("reply text = %q, want echo: hello", replies[0].text)
	}
}

// TestIntegration_MultipleMessages verifies sequential message processing within a session.
func TestIntegration_MultipleMessages(t *testing.T) {
	mock := &mockBackend{}

	var repliesMu sync.Mutex
	var replies []string

	mgr := NewManager(ManagerOpts{
		NewBackend: func(ctx context.Context) (agent.Backend, error) {
			return mock, nil
		},
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

	// Send 3 messages to same session
	for _, msg := range []string{"msg1", "msg2", "msg3"} {
		if err := mgr.Enqueue("u:user1", PendingMessage{Text: msg, ContextToken: "user1"}); err != nil {
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

// TestIntegration_MultipleSessions verifies independent sessions for different users.
func TestIntegration_MultipleSessions(t *testing.T) {
	var backendsMu sync.Mutex
	backends := make(map[string]*mockBackend)

	var repliesMu sync.Mutex
	var replies []struct{ key, text string }

	mgr := NewManager(ManagerOpts{
		NewBackend: func(ctx context.Context) (agent.Backend, error) {
			m := &mockBackend{}
			// We'll track which backend was created, but can't know key here
			backendsMu.Lock()
			backends[time.Now().String()] = m
			backendsMu.Unlock()
			return m, nil
		},
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

	// Send messages to two different users
	_ = mgr.Enqueue("u:alice", PendingMessage{Text: "hi from alice", ContextToken: "alice"})
	_ = mgr.Enqueue("u:bob", PendingMessage{Text: "hi from bob", ContextToken: "bob"})

	time.Sleep(500 * time.Millisecond)

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if len(replies) != 2 {
		t.Fatalf("replies count = %d, want 2", len(replies))
	}

	// Check both users got replies (order may vary due to goroutines)
	found := map[string]bool{}
	for _, r := range replies {
		found[r.key] = true
	}
	if !found["u:alice"] || !found["u:bob"] {
		t.Errorf("replies = %v, want both u:alice and u:bob", replies)
	}
}

// TestIntegration_WithStore verifies session persistence integration.
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
		NewBackend: func(ctx context.Context) (agent.Backend, error) {
			return mock, nil
		},
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

	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "persist me", ContextToken: "user1"})
	time.Sleep(300 * time.Millisecond)

	if !replied.Load() {
		t.Fatal("expected reply callback")
	}

	// Verify session was persisted
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

	// Verify messages were persisted
	msgs, err := store.LoadRecentMessages("u:user1", 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages count = %d, want 2 (user + assistant)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "persist me" {
		t.Errorf("msgs[0] = %+v, want user/persist me", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "echo: persist me" {
		t.Errorf("msgs[1] = %+v, want assistant/echo: persist me", msgs[1])
	}
}

// TestIntegration_MaxConcurrentEviction verifies that exceeding max sessions triggers eviction.
func TestIntegration_MaxConcurrentEviction(t *testing.T) {
	var repliesMu sync.Mutex
	replyCount := 0

	mgr := NewManager(ManagerOpts{
		NewBackend: func(ctx context.Context) (agent.Backend, error) {
			return &mockBackend{delay: 10 * time.Millisecond}, nil
		},
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

	// Fill up to max
	_ = mgr.Enqueue("u:user1", PendingMessage{Text: "a", ContextToken: "u1"})
	_ = mgr.Enqueue("u:user2", PendingMessage{Text: "b", ContextToken: "u2"})
	time.Sleep(200 * time.Millisecond)

	// Third session should trigger eviction of oldest
	_ = mgr.Enqueue("u:user3", PendingMessage{Text: "c", ContextToken: "u3"})
	time.Sleep(200 * time.Millisecond)

	repliesMu.Lock()
	defer repliesMu.Unlock()
	if replyCount != 3 {
		t.Errorf("replyCount = %d, want 3", replyCount)
	}
}
