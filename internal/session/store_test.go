package session

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer s.Close()
}

func TestUpsertAndGetSession(t *testing.T) {
	s := openTestStore(t)

	if err := s.UpsertSession("u:alice", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	sess, err := s.GetSession("u:alice")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess == nil {
		t.Fatal("GetSession returned nil for existing key")
	}
	if sess.Key != "u:alice" {
		t.Errorf("Key = %q, want %q", sess.Key, "u:alice")
	}
	if sess.State != StateActive {
		t.Errorf("State = %q, want %q", sess.State, StateActive)
	}
	if sess.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if sess.LastActivity.IsZero() {
		t.Error("LastActivity should not be zero")
	}
}

func TestGetSessionNonExistent(t *testing.T) {
	s := openTestStore(t)

	sess, err := s.GetSession("u:nobody")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil for non-existent key, got %+v", sess)
	}
}

func TestUpdateActivity(t *testing.T) {
	s := openTestStore(t)

	if err := s.UpsertSession("u:bob", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	before, _ := s.GetSession("u:bob")
	time.Sleep(10 * time.Millisecond)

	if err := s.UpdateActivity("u:bob"); err != nil {
		t.Fatalf("UpdateActivity: %v", err)
	}

	after, _ := s.GetSession("u:bob")
	if !after.LastActivity.After(before.LastActivity) {
		t.Errorf("LastActivity should have increased: before=%v after=%v",
			before.LastActivity, after.LastActivity)
	}
}

func TestUpdateState(t *testing.T) {
	s := openTestStore(t)

	if err := s.UpsertSession("u:carol", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	if err := s.UpdateState("u:carol", StateSuspended); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	sess, _ := s.GetSession("u:carol")
	if sess.State != StateSuspended {
		t.Errorf("State = %q, want %q", sess.State, StateSuspended)
	}
}

func TestListActiveSessions(t *testing.T) {
	s := openTestStore(t)

	// Create sessions with different states
	if err := s.UpsertSession("u:active1", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if err := s.UpsertSession("u:active2", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if err := s.UpsertSession("u:suspended1", StateSuspended); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	active, err := s.ListActiveSessions()
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}

	if len(active) != 2 {
		t.Fatalf("got %d active sessions, want 2", len(active))
	}

	keys := map[string]bool{}
	for _, sess := range active {
		keys[sess.Key] = true
		if sess.State != StateActive {
			t.Errorf("session %s has state %q, want %q", sess.Key, sess.State, StateActive)
		}
	}
	if !keys["u:active1"] || !keys["u:active2"] {
		t.Errorf("expected u:active1 and u:active2 in results, got %v", keys)
	}
}

func TestAppendAndLoadRecentMessages(t *testing.T) {
	s := openTestStore(t)

	key := "u:dave"
	if err := s.UpsertSession(key, StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	msgs := []struct {
		role    string
		content string
	}{
		{"user", "hello"},
		{"assistant", "hi there"},
		{"user", "how are you"},
	}

	for _, m := range msgs {
		if err := s.AppendMessage(key, m.role, m.content); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	}

	loaded, err := s.LoadRecentMessages(key, 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages: %v", err)
	}

	if len(loaded) != 3 {
		t.Fatalf("got %d messages, want 3", len(loaded))
	}

	// Verify oldest-first order
	if loaded[0].Content != "hello" {
		t.Errorf("first message content = %q, want %q", loaded[0].Content, "hello")
	}
	if loaded[1].Role != "assistant" {
		t.Errorf("second message role = %q, want %q", loaded[1].Role, "assistant")
	}
	if loaded[2].Content != "how are you" {
		t.Errorf("third message content = %q, want %q", loaded[2].Content, "how are you")
	}
}

func TestLoadRecentMessagesWithLimit(t *testing.T) {
	s := openTestStore(t)

	key := "u:eve"
	if err := s.UpsertSession(key, StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	for i := 0; i < 5; i++ {
		content := string(rune('a' + i)) // "a", "b", "c", "d", "e"
		if err := s.AppendMessage(key, "user", content); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	loaded, err := s.LoadRecentMessages(key, 3)
	if err != nil {
		t.Fatalf("LoadRecentMessages: %v", err)
	}

	if len(loaded) != 3 {
		t.Fatalf("got %d messages, want 3", len(loaded))
	}

	// Should be the 3 most recent, in oldest-first order: "c", "d", "e"
	expected := []string{"c", "d", "e"}
	for i, want := range expected {
		if loaded[i].Content != want {
			t.Errorf("message[%d].Content = %q, want %q", i, loaded[i].Content, want)
		}
	}
}

func TestDeleteMessages(t *testing.T) {
	s := openTestStore(t)

	key := "u:frank"
	if err := s.UpsertSession(key, StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	if err := s.AppendMessage(key, "user", "msg1"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.AppendMessage(key, "assistant", "msg2"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if err := s.DeleteMessages(key); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	loaded, err := s.LoadRecentMessages(key, 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("got %d messages after delete, want 0", len(loaded))
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s := openTestStore(t)

	// Create a suspended session
	if err := s.UpsertSession("u:old", StateSuspended); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	// Add a message to verify cascade cleanup
	if err := s.AppendMessage("u:old", "user", "ancient msg"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Create an active session (should not be deleted)
	if err := s.UpsertSession("u:fresh", StateActive); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Delete sessions older than 0 duration (everything suspended)
	n, err := s.DeleteExpiredSessions(0)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d sessions, want 1", n)
	}

	// Verify old session is gone
	sess, _ := s.GetSession("u:old")
	if sess != nil {
		t.Error("expected old session to be deleted")
	}

	// Verify active session still exists
	sess, _ = s.GetSession("u:fresh")
	if sess == nil {
		t.Error("expected active session to still exist")
	}

	// Verify messages were also deleted
	msgs, _ := s.LoadRecentMessages("u:old", 10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for deleted session, got %d", len(msgs))
	}
}
