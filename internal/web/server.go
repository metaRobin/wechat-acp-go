package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/metaRobin/wechat-router-go/internal/session"
)

// Server is the embedded HTTP management server.
type Server struct {
	mgr    *session.Manager
	store  *session.Store
	addr   string
	logger *slog.Logger

	mu  sync.Mutex
	srv *http.Server
}

// New creates a new Server. store may be nil (active-only mode).
func New(mgr *session.Manager, store *session.Store, addr string, logger *slog.Logger) *Server {
	return &Server{
		mgr:    mgr,
		store:  store,
		addr:   addr,
		logger: logger,
	}
}

// Start registers routes and begins serving in a background goroutine.
// It returns immediately; the server runs until ctx is cancelled.
func (s *Server) Start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/", s.handleIndex)

	s.mu.Lock()
	s.srv = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.mu.Unlock()

	go func() {
		s.logger.Info("web_server_starting", "addr", s.addr, "url", "http://"+s.addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("web_server_failed", "addr", s.addr, "error", err)
			fmt.Printf("[web] 启动失败: %v (地址: %s)\n", err, s.addr)
		}
	}()

	go func() {
		<-ctx.Done()
		s.Stop()
	}()
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()

	if srv == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("web_server_shutdown_error", "error", err)
	}
	s.logger.Info("web_server_stopped")
}
