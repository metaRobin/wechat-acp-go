package web

import (
	"encoding/json"
	"net/http"
	"time"
)

// SessionSummary is the API response for a single session in the list.
type SessionSummary struct {
	Key          string    `json:"key"`
	AgentID      string    `json:"agentID"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActivity time.Time `json:"lastActivity"`
	Active       bool      `json:"active"`
}

// SessionDetail is the API response for a single session with messages.
type SessionDetail struct {
	SessionSummary
	Messages []MessageItem `json:"messages"`
}

// MessageItem represents one message in the conversation history.
type MessageItem struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	key := r.URL.Query().Get("key")

	switch r.Method {
	case http.MethodGet:
		if key == "" {
			s.listSessions(w, r)
		} else {
			s.getSession(w, r, key)
		}
	case http.MethodDelete:
		if key == "" {
			writeError(w, http.StatusBadRequest, "key required")
			return
		}
		s.deleteSession(w, r, key)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	// Start with active in-memory sessions
	active := s.mgr.ListSessions()
	seen := make(map[string]bool, len(active))
	out := make([]SessionSummary, 0, len(active))
	for _, si := range active {
		seen[si.Key] = true
		out = append(out, SessionSummary{
			Key:          si.Key,
			AgentID:      si.AgentID,
			CreatedAt:    si.CreatedAt,
			LastActivity: si.LastActivity,
			Active:       true,
		})
	}

	// Merge stored sessions (includes historical/suspended)
	if s.store != nil {
		stored, err := s.store.ListAllSessions(100)
		if err == nil {
			for _, ss := range stored {
				if seen[ss.Key] {
					continue
				}
				out = append(out, SessionSummary{
					Key:          ss.Key,
					AgentID:      ss.AgentID,
					CreatedAt:    ss.CreatedAt,
					LastActivity: ss.LastActivity,
					Active:       false,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getSession(w http.ResponseWriter, _ *http.Request, key string) {
	// Find in active sessions first
	active := s.mgr.ListSessions()
	var found *SessionSummary
	for _, si := range active {
		if si.Key == key {
			ss := SessionSummary{
				Key:          si.Key,
				AgentID:      si.AgentID,
				CreatedAt:    si.CreatedAt,
				LastActivity: si.LastActivity,
			}
			found = &ss
			break
		}
	}

	// Fall back to store for suspended sessions
	if found == nil && s.store != nil {
		stored, err := s.store.GetSession(key)
		if err != nil || stored == nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		found = &SessionSummary{
			Key:          stored.Key,
			AgentID:      stored.AgentID,
			CreatedAt:    stored.CreatedAt,
			LastActivity: stored.LastActivity,
		}
	}

	if found == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	detail := SessionDetail{SessionSummary: *found}

	// Load messages from store
	if s.store != nil {
		msgs, err := s.store.LoadRecentMessages(key, 50)
		if err == nil {
			for _, m := range msgs {
				detail.Messages = append(detail.Messages, MessageItem{
					Role:      m.Role,
					Content:   m.Content,
					CreatedAt: m.CreatedAt,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) deleteSession(w http.ResponseWriter, _ *http.Request, key string) {
	s.mgr.RemoveSession(key)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleIndex serves the embedded HTML frontend.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, staticFiles, "static/index.html")
}

