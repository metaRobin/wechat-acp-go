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

	"github.com/metaRobin/wechat-acp-go/internal/agent"
)

// PendingMessage is a message waiting to be processed.
type PendingMessage struct {
	Text         string // plain text prompt for the agent
	ContextToken string // WeChat reply target
}

// UserSession represents a single active session.
type UserSession struct {
	Key          string
	Backend      agent.Backend
	MsgCh        chan PendingMessage
	lastActivity atomic.Int64 // unix millis
	CreatedAt    time.Time
	ctx          context.Context
	cancel       context.CancelFunc
}

// BackendFactory creates a Backend for a new session.
type BackendFactory func(ctx context.Context) (agent.Backend, error)

// ManagerOpts configures the SessionManager.
type ManagerOpts struct {
	NewBackend    BackendFactory
	IdleTimeout   time.Duration
	MaxConcurrent int

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

// Enqueue adds a message to the session's queue. Creates a new session if needed.
func (m *Manager) Enqueue(sessionKey string, msg PendingMessage) error {
	m.mu.Lock()
	sess, exists := m.sessions[sessionKey]
	if !exists {
		if len(m.sessions) >= m.opts.MaxConcurrent {
			m.evictOldestLocked()
		}
		var err error
		sess, err = m.createSessionLocked(sessionKey)
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("create session %s: %w", sessionKey, err)
		}
	}
	sess.lastActivity.Store(time.Now().UnixMilli())
	m.mu.Unlock()

	select {
	case sess.MsgCh <- msg:
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
	return nil
}

func (m *Manager) createSessionLocked(key string) (*UserSession, error) {
	sessCtx, sessCancel := context.WithCancel(m.ctx)

	backend, err := m.opts.NewBackend(sessCtx)
	if err != nil {
		sessCancel()
		return nil, err
	}

	sess := &UserSession{
		Key:       key,
		Backend:   backend,
		MsgCh:     make(chan PendingMessage, 32),
		CreatedAt: time.Now(),
		ctx:       sessCtx,
		cancel:    sessCancel,
	}
	sess.lastActivity.Store(time.Now().UnixMilli())

	m.sessions[key] = sess
	go m.consumeLoop(sess)

	if m.opts.Store != nil {
		_ = m.opts.Store.UpsertSession(key, StateActive)
	}

	m.opts.Logger.Info("session_created", "key", key)
	return sess, nil
}

func (m *Manager) consumeLoop(sess *UserSession) {
	defer func() {
		m.mu.Lock()
		delete(m.sessions, sess.Key)
		m.mu.Unlock()
		sess.cancel()
	}()

	for {
		select {
		case <-sess.ctx.Done():
			return
		case msg, ok := <-sess.MsgCh:
			if !ok {
				return
			}
			m.processMessage(sess, msg)
		}
	}
}

func (m *Manager) processMessage(sess *UserSession, msg PendingMessage) {
	// Send typing indicator
	if m.opts.SendTyping != nil {
		m.opts.SendTyping(sess.Key, msg.ContextToken)
	}

	// Send prompt to agent backend
	var replyParts []string
	result, err := sess.Backend.Prompt(sess.ctx, msg.Text, func(chunk string) {
		// Stream callback: send typing on chunks
		if m.opts.SendTyping != nil {
			m.opts.SendTyping(sess.Key, msg.ContextToken)
		}
	})

	if err != nil {
		if sess.ctx.Err() != nil {
			return // session cancelled
		}
		m.opts.Logger.Error("prompt_failed", "key", sess.Key, "error", err)
		if m.opts.OnReply != nil {
			m.opts.OnReply(sess.Key, msg.ContextToken, "Agent error: "+err.Error())
		}
		return
	}

	replyText := result.Text
	if result.StopReason == "cancelled" {
		replyParts = append(replyParts, replyText, "\n[cancelled]")
		replyText = strings.Join(replyParts, "")
	} else if result.StopReason == "refusal" {
		replyParts = append(replyParts, replyText, "\n[agent refused to continue]")
		replyText = strings.Join(replyParts, "")
	}

	if strings.TrimSpace(replyText) != "" && m.opts.OnReply != nil {
		m.opts.OnReply(sess.Key, msg.ContextToken, replyText)
	}

	sess.lastActivity.Store(time.Now().UnixMilli())

	// Persist
	if m.opts.Store != nil {
		_ = m.opts.Store.AppendMessage(sess.Key, "user", msg.Text)
		if strings.TrimSpace(replyText) != "" {
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
