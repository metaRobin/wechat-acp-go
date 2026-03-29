package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SessionState represents the lifecycle state of a session.
type SessionState string

const (
	StateActive    SessionState = "active"
	StateSuspended SessionState = "suspended"
)

// StoredSession represents a session record in the database.
type StoredSession struct {
	Key          string
	State        SessionState
	AgentID      string
	LastActivity time.Time
	CreatedAt    time.Time
}

// StoredMessage represents a message in the session history.
type StoredMessage struct {
	ID        int64
	SessionKey string
	Role      string // "user" or "assistant"
	Content   string // JSON-encoded content blocks
	CreatedAt time.Time
}

// Store provides session persistence via SQLite.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates a SQLite database for session storage.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	// WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	s := &Store{db: db}
	if err := s.createSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		key          TEXT PRIMARY KEY,
		state        TEXT NOT NULL DEFAULT 'active',
		agent_id     TEXT NOT NULL DEFAULT '',
		last_activity INTEGER NOT NULL,
		created_at   INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS session_messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_key TEXT NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT NOT NULL,
		created_at  INTEGER NOT NULL,
		FOREIGN KEY (session_key) REFERENCES sessions(key) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_session ON session_messages(session_key, created_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// GetSession retrieves a session by key.
func (s *Store) GetSession(key string) (*StoredSession, error) {
	row := s.db.QueryRow(
		"SELECT key, state, agent_id, last_activity, created_at FROM sessions WHERE key = ?", key)

	var sess StoredSession
	var lastActivity, createdAt int64
	err := row.Scan(&sess.Key, &sess.State, &sess.AgentID, &lastActivity, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", key, err)
	}
	sess.LastActivity = time.UnixMilli(lastActivity)
	sess.CreatedAt = time.UnixMilli(createdAt)
	return &sess, nil
}

// UpsertSession creates or updates a session record.
func (s *Store) UpsertSession(key string, state SessionState, agentID string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		INSERT INTO sessions (key, state, agent_id, last_activity, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET state = ?, agent_id = ?, last_activity = ?`,
		key, state, agentID, now, now, state, agentID, now)
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", key, err)
	}
	return nil
}

// UpdateActivity updates the last_activity timestamp for a session.
func (s *Store) UpdateActivity(key string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec("UPDATE sessions SET last_activity = ? WHERE key = ?", now, key)
	if err != nil {
		return fmt.Errorf("update activity %s: %w", key, err)
	}
	return nil
}

// UpdateState updates the state of a session.
func (s *Store) UpdateState(key string, state SessionState) error {
	_, err := s.db.Exec("UPDATE sessions SET state = ? WHERE key = ?", state, key)
	if err != nil {
		return fmt.Errorf("update state %s: %w", key, err)
	}
	return nil
}

// ListActiveSessions returns all sessions in the given state.
func (s *Store) ListActiveSessions() ([]StoredSession, error) {
	rows, err := s.db.Query(
		"SELECT key, state, agent_id, last_activity, created_at FROM sessions WHERE state = ?",
		StateActive)
	if err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []StoredSession
	for rows.Next() {
		var sess StoredSession
		var lastActivity, createdAt int64
		if err := rows.Scan(&sess.Key, &sess.State, &sess.AgentID, &lastActivity, &createdAt); err != nil {
			return nil, err
		}
		sess.LastActivity = time.UnixMilli(lastActivity)
		sess.CreatedAt = time.UnixMilli(createdAt)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// AppendMessage stores a message in the session history.
func (s *Store) AppendMessage(sessionKey, role, content string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO session_messages (session_key, role, content, created_at) VALUES (?, ?, ?, ?)",
		sessionKey, role, content, now)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

// LoadRecentMessages returns the most recent messages for a session, ordered oldest first.
func (s *Store) LoadRecentMessages(sessionKey string, limit int) ([]StoredMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, session_key, role, content, created_at
		FROM session_messages
		WHERE session_key = ?
		ORDER BY created_at DESC
		LIMIT ?`, sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var createdAt int64
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(createdAt)
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to oldest-first order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// DeleteMessages removes all messages for a session.
func (s *Store) DeleteMessages(sessionKey string) error {
	_, err := s.db.Exec("DELETE FROM session_messages WHERE session_key = ?", sessionKey)
	if err != nil {
		return fmt.Errorf("delete messages %s: %w", sessionKey, err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions that have been suspended for longer than maxAge.
func (s *Store) DeleteExpiredSessions(maxAge time.Duration) (int64, error) {
	threshold := time.Now().Add(-maxAge).UnixMilli()

	// Delete messages first (foreign key)
	_, err := s.db.Exec(`
		DELETE FROM session_messages WHERE session_key IN (
			SELECT key FROM sessions WHERE state = ? AND last_activity < ?
		)`, StateSuspended, threshold)
	if err != nil {
		return 0, fmt.Errorf("delete expired messages: %w", err)
	}

	result, err := s.db.Exec(
		"DELETE FROM sessions WHERE state = ? AND last_activity < ?",
		StateSuspended, threshold)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return result.RowsAffected()
}

// ContentToJSON serializes content blocks to JSON for storage.
func ContentToJSON(content interface{}) string {
	data, _ := json.Marshal(content)
	return string(data)
}
