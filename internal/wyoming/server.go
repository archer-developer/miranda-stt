package wyoming

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

// Server listens for Wyoming TCP connections and spawns a Session per client.
type Server struct {
	listener    net.Listener
	logger      *slog.Logger
	apiKey      string
	model       string
	languages   []string
	audioDumpDir string
	turnTimeout time.Duration
	sessionSeq  atomic.Uint64
}

// NewServer creates a Server bound to addr.
func NewServer(
	addr string,
	logger *slog.Logger,
	apiKey, model string,
	languages []string,
	audioDumpDir string,
	turnTimeoutMs int,
) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wyoming/server: listen %s: %w", addr, err)
	}
	return &Server{
		listener:    ln,
		logger:      logger,
		apiKey:      apiKey,
		model:       model,
		languages:   languages,
		audioDumpDir: audioDumpDir,
		turnTimeout: time.Duration(turnTimeoutMs) * time.Millisecond,
	}, nil
}

// Addr returns the local address the listener is bound to.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Serve accepts connections until ctx is cancelled or the listener is closed.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // clean shutdown
			default:
				return fmt.Errorf("wyoming/server: accept: %w", err)
			}
		}

		id := fmt.Sprintf("%06d", s.sessionSeq.Add(1))
		sess := NewSession(id, conn, s.logger, s.apiKey, s.model, s.languages, s.audioDumpDir, s.turnTimeout)
		s.logger.Info("client connected", "session_id", id, "remote", conn.RemoteAddr())
		go sess.Run(ctx)
	}
}
