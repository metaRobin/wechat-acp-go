package session

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/metaRobin/wechat-router-go/internal/adapter"
	"github.com/metaRobin/wechat-router-go/internal/agent"
)

// PendingMessage is a message waiting to be processed.
type PendingMessage struct {
	Text         string    // plain text prompt for the agent
	ContextToken string    // WeChat reply target
	EnqueuedAt   time.Time // time the message entered the queue
}

// UserSession represents a single active session.
type UserSession struct {
	Key              string
	AgentID          string
	Backend          agent.Backend
	MsgCh            chan PendingMessage
	lastActivity     atomic.Int64 // unix millis
	historyInjected  bool
	CreatedAt        time.Time
	ctx              context.Context
	cancel           context.CancelFunc
}

// BackendFactory creates a Backend for a new session.
type BackendFactory func(ctx context.Context) (agent.Backend, error)

// BackendRegistry maps agent IDs to their BackendFactory.
type BackendRegistry map[string]BackendFactory

// ManagerOpts configures the SessionManager.
type ManagerOpts struct {
	Registry      BackendRegistry
	DefaultAgent  string // optional: auto-use this agent if set
	HistoryLimit    int // max messages to restore (default 20)
	StreamThreshold int    // chars before streaming flush (default 500, 0=disable)
	MediaDir        string // directory for saving media files
	IdleTimeout     time.Duration
	MaxConcurrent int
	QueueSize     int           // max pending messages per session (default 3)
	QueueTimeout  time.Duration // max time a message waits in queue (default 2m)

	// Callbacks
	OnReply    func(sessionKey, contextToken, text string)
	SendTyping func(sessionKey, contextToken string)

	// Persistence (optional)
	Store *Store

	Logger *slog.Logger
}

// Manager manages user sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*UserSession
	opts     ManagerOpts
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewManager(opts ManagerOpts) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		sessions: make(map[string]*UserSession),
		opts:     opts,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the cleanup ticker and marks previously active sessions as suspended.
func (m *Manager) Start() {
	if m.opts.Store != nil {
		active, err := m.opts.Store.ListActiveSessions()
		if err == nil {
			for _, s := range active {
				_ = m.opts.Store.UpdateState(s.Key, StateSuspended)
			}
			if len(active) > 0 {
				m.opts.Logger.Info("sessions_suspended_on_startup", "count", len(active))
			}
		}
		if n, err := m.opts.Store.DeleteExpiredSessions(7 * 24 * time.Hour); err == nil && n > 0 {
			m.opts.Logger.Info("expired_sessions_deleted", "count", n)
		}
	}
	go m.cleanupLoop()
}

// Stop terminates all sessions and agent processes.
func (m *Manager) Stop() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sess := range m.sessions {
		sess.cancel()
		if sess.Backend != nil {
			sess.Backend.Kill()
		}
	}
	m.sessions = make(map[string]*UserSession)
}

// SessionInfo is a snapshot of a session for external consumers (e.g., web UI).
type SessionInfo struct {
	Key          string    `json:"key"`
	AgentID      string    `json:"agentID"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActivity time.Time `json:"lastActivity"`
}

// ListSessions returns a snapshot of all currently active sessions.
func (m *Manager) ListSessions() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, sess := range m.sessions {
		out = append(out, SessionInfo{
			Key:          sess.Key,
			AgentID:      sess.AgentID,
			CreatedAt:    sess.CreatedAt,
			LastActivity: time.UnixMilli(sess.lastActivity.Load()),
		})
	}
	return out
}

// HasSession returns true if a session exists for the given key.
func (m *Manager) HasSession(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sessions[key]
	return ok
}

// GetSessionAgentID returns the agent ID for an existing session, or "" if none.
func (m *Manager) GetSessionAgentID(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if sess, ok := m.sessions[key]; ok {
		return sess.AgentID
	}
	return ""
}

// DefaultAgent returns the configured default agent ID.
func (m *Manager) DefaultAgent() string {
	return m.opts.DefaultAgent
}

// HasAgent checks if an agent ID exists in the registry.
func (m *Manager) HasAgent(agentID string) bool {
	_, ok := m.opts.Registry[agentID]
	return ok
}

// Enqueue adds a message to the session's queue. Creates a new session if needed.
// agentID specifies which agent to use; empty string uses the session's existing agent.
// Returns a non-nil busy reply string if the queue is full; the caller should forward it to the user.
func (m *Manager) Enqueue(sessionKey string, msg PendingMessage, agentID string) (string, error) {
	msg.EnqueuedAt = time.Now()

	m.mu.Lock()
	sess, exists := m.sessions[sessionKey]
	if !exists {
		if agentID == "" {
			m.mu.Unlock()
			return "", fmt.Errorf("no agent selected")
		}
		if len(m.sessions) >= m.opts.MaxConcurrent {
			m.evictOldestLocked()
		}
		var err error
		sess, err = m.createSessionLocked(sessionKey, agentID)
		if err != nil {
			m.mu.Unlock()
			return "", fmt.Errorf("create session %s: %w", sessionKey, err)
		}
	}
	sess.lastActivity.Store(time.Now().UnixMilli())
	m.mu.Unlock()

	select {
	case sess.MsgCh <- msg:
		return "", nil
	case <-m.ctx.Done():
		return "", m.ctx.Err()
	default:
		// Queue is full — reject immediately
		return "⏳ 正在处理中，请稍后再试", nil
	}
}

// SwitchAgent kills the current session and creates a new one with a different agent.
func (m *Manager) SwitchAgent(sessionKey, newAgentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Kill existing session if present
	if sess, ok := m.sessions[sessionKey]; ok {
		sess.cancel()
		if sess.Backend != nil {
			sess.Backend.Kill()
		}
		delete(m.sessions, sessionKey)
	}

	// Create new session with the new agent
	if len(m.sessions) >= m.opts.MaxConcurrent {
		m.evictOldestLocked()
	}
	_, err := m.createSessionLocked(sessionKey, newAgentID)
	return err
}

func (m *Manager) createSessionLocked(key, agentID string) (*UserSession, error) {
	factory, ok := m.opts.Registry[agentID]
	if !ok {
		return nil, fmt.Errorf("unknown agent: %s", agentID)
	}

	sessCtx, sessCancel := context.WithCancel(m.ctx)

	backend, err := factory(sessCtx)
	if err != nil {
		sessCancel()
		return nil, err
	}

	queueSize := m.opts.QueueSize
	if queueSize <= 0 {
		queueSize = 3
	}
	sess := &UserSession{
		Key:       key,
		AgentID:   agentID,
		Backend:   backend,
		MsgCh:     make(chan PendingMessage, queueSize),
		CreatedAt: time.Now(),
		ctx:       sessCtx,
		cancel:    sessCancel,
	}
	sess.lastActivity.Store(time.Now().UnixMilli())

	m.sessions[key] = sess
	go m.consumeLoop(sess)

	if m.opts.Store != nil {
		_ = m.opts.Store.UpsertSession(key, StateActive, agentID)
	}

	m.opts.Logger.Info("session_created", "key", key, "agent", agentID)
	return sess, nil
}

func (m *Manager) consumeLoop(sess *UserSession) {
	defer func() {
		m.mu.Lock()
		delete(m.sessions, sess.Key)
		m.mu.Unlock()
		sess.cancel()
	}()

	queueTimeout := m.opts.QueueTimeout
	if queueTimeout <= 0 {
		queueTimeout = 2 * time.Minute
	}

	for {
		select {
		case <-sess.ctx.Done():
			return
		case msg, ok := <-sess.MsgCh:
			if !ok {
				return
			}
			if time.Since(msg.EnqueuedAt) > queueTimeout {
				if m.opts.OnReply != nil {
					m.opts.OnReply(sess.Key, msg.ContextToken, "⏰ 请求超时，请重新发送")
				}
				continue
			}
			m.processMessage(sess, msg)
		}
	}
}

func (m *Manager) processMessage(sess *UserSession, msg PendingMessage) {
	if m.opts.SendTyping != nil {
		m.opts.SendTyping(sess.Key, msg.ContextToken)
	}

	// Typing heartbeat: resend every 15s while agent is processing
	done := make(chan struct{})
	defer close(done)
	if m.opts.SendTyping != nil {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					m.opts.SendTyping(sess.Key, msg.ContextToken)
				}
			}
		}()
	}

	// Inject history on first message of a restored session
	promptText := msg.Text
	if !sess.historyInjected {
		sess.historyInjected = true
		if history := m.formatHistory(sess.Key); history != "" {
			promptText = history + "\n\n" + promptText
			m.opts.Logger.Debug("history_injected", "key", sess.Key)
		}
	}

	// Create stream buffer for real-time reply sending
	sendFn := func(text string) {
		if m.opts.OnReply != nil {
			m.opts.OnReply(sess.Key, msg.ContextToken, text)
		}
	}
	buf := NewStreamBuffer(m.opts.StreamThreshold, sendFn)

	result, err := sess.Backend.Prompt(sess.ctx, promptText, func(chunk string) {
		buf.Write(chunk)
		if m.opts.SendTyping != nil {
			m.opts.SendTyping(sess.Key, msg.ContextToken)
		}
	})

	if err != nil {
		if sess.ctx.Err() != nil {
			return
		}
		m.opts.Logger.Error("prompt_failed", "key", sess.Key, "error", err)
		if m.opts.OnReply != nil {
			m.opts.OnReply(sess.Key, msg.ContextToken, "Agent error: "+err.Error())
		}
		return
	}

	// Flush remaining buffered text
	buf.Flush()

	// Append stop reason if needed
	if result.StopReason == "cancelled" && m.opts.OnReply != nil {
		m.opts.OnReply(sess.Key, msg.ContextToken, "[cancelled]")
	} else if result.StopReason == "refusal" && m.opts.OnReply != nil {
		m.opts.OnReply(sess.Key, msg.ContextToken, "[agent refused to continue]")
	}

	sess.lastActivity.Store(time.Now().UnixMilli())

	// Persist full reply text
	replyText := buf.AllText()
	if m.opts.Store != nil {
		_ = m.opts.Store.AppendMessage(sess.Key, "user", msg.Text)
		if replyText != "" {
			_ = m.opts.Store.AppendMessage(sess.Key, "assistant", replyText)
		}
		_ = m.opts.Store.UpdateActivity(sess.Key)
	}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupIdleSessions()
		}
	}
}

func (m *Manager) cleanupIdleSessions() {
	if m.opts.IdleTimeout <= 0 {
		return
	}
	now := time.Now().UnixMilli()
	threshold := m.opts.IdleTimeout.Milliseconds()

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, sess := range m.sessions {
		idle := now - sess.lastActivity.Load()
		if idle > threshold && len(sess.MsgCh) == 0 {
			m.opts.Logger.Info("session_idle_cleanup", "key", key)
			sess.cancel()
			if sess.Backend != nil {
				sess.Backend.Kill()
			}
			delete(m.sessions, key)

			if m.opts.Store != nil {
				_ = m.opts.Store.UpdateState(key, StateSuspended)
			}
			if m.opts.MediaDir != "" {
				adapter.CleanupMedia(m.opts.MediaDir, key)
			}
		}
	}
}

func (m *Manager) evictOldestLocked() {
	var oldestKey string
	var oldestActivity int64 = math.MaxInt64
	for key, sess := range m.sessions {
		if len(sess.MsgCh) == 0 && sess.lastActivity.Load() < oldestActivity {
			oldestActivity = sess.lastActivity.Load()
			oldestKey = key
		}
	}
	if oldestKey != "" {
		sess := m.sessions[oldestKey]
		m.opts.Logger.Info("session_evicted", "key", oldestKey)
		sess.cancel()
		if sess.Backend != nil {
			sess.Backend.Kill()
		}
		delete(m.sessions, oldestKey)
	}
}

// formatHistory loads recent messages from store and formats them as context.
func (m *Manager) formatHistory(sessionKey string) string {
	if m.opts.Store == nil {
		return ""
	}
	limit := m.opts.HistoryLimit
	if limit <= 0 {
		limit = 20
	}
	msgs, err := m.opts.Store.LoadRecentMessages(sessionKey, limit)
	if err != nil || len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[以下是之前的对话历史]\n")
	for _, msg := range msgs {
		role := "User"
		if msg.Role == "assistant" {
			role = "Assistant"
		}
		sb.WriteString(role + ": " + msg.Content + "\n")
	}
	sb.WriteString("[对话历史结束]")
	return sb.String()
}

// CancelSession cancels the current in-flight agent request for the given session key,
// drains the pending queue, and removes the session so it can be recreated on next message.
// Returns a message to send back to the user.
func (m *Manager) CancelSession(sessionKey string) string {
	m.mu.Lock()
	sess, ok := m.sessions[sessionKey]
	if !ok {
		m.mu.Unlock()
		return "没有正在执行的请求"
	}
	sess.cancel()
	if sess.Backend != nil {
		sess.Backend.Kill()
	}
	// Drain pending messages
	for {
		select {
		case <-sess.MsgCh:
		default:
			goto drained
		}
	}
drained:
	delete(m.sessions, sessionKey)
	m.mu.Unlock()
	return "🛑 已取消当前请求"
}

// RemoveSession kills the backend, removes the session, and deletes its messages and media from store.
func (m *Manager) RemoveSession(sessionKey string) {
	m.mu.Lock()
	if sess, ok := m.sessions[sessionKey]; ok {
		sess.cancel()
		if sess.Backend != nil {
			sess.Backend.Kill()
		}
		delete(m.sessions, sessionKey)
	}
	m.mu.Unlock()

	if m.opts.Store != nil {
		_ = m.opts.Store.DeleteMessages(sessionKey)
	}

	// Clean up media files
	if m.opts.MediaDir != "" {
		adapter.CleanupMedia(m.opts.MediaDir, sessionKey)
	}
}
