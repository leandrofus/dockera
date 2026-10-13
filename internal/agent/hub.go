package agent

import (
	"context"
	"dockler/internal/i18n"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Hub manages active agent sessions established via outbound reverse tunnels.
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*yamux.Session
}

// DefaultHub is the singleton agent hub used by the dockler server and client.
var DefaultHub = NewHub()

func NewHub() *Hub {
	return &Hub{
		sessions: make(map[string]*yamux.Session),
	}
}

// Register stores an active agent session under the given name, closing any
// existing session with the same name.
func (h *Hub) Register(name string, s *yamux.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if prev, ok := h.sessions[name]; ok && prev != s {
		_ = prev.Close()
	}
	h.sessions[name] = s
}

// Unregister removes the session if it matches the current one for name.
func (h *Hub) Unregister(name string, s *yamux.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if curr, ok := h.sessions[name]; ok && curr == s {
		delete(h.sessions, name)
	}
}

// IsOnline checks whether an agent session is active and not closed.
func (h *Hub) IsOnline(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[name]
	return ok && !s.IsClosed()
}

// ListOnline returns all currently connected agent names.
func (h *Hub) ListOnline() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []string
	for name, s := range h.sessions {
		if !s.IsClosed() {
			out = append(out, name)
		}
	}
	return out
}

// Dial opens a multiplexed stream to the agent with the given name.
// The returned net.Conn is directly connected to the remote agent's Docker daemon socket.
func (h *Hub) Dial(ctx context.Context, name string) (net.Conn, error) {
	h.mu.RLock()
	s, ok := h.sessions[name]
	h.mu.RUnlock()

	if !ok || s.IsClosed() {
		return nil, i18n.Errorf("agent %s is offline (waiting for connection)", name)
	}

	type streamResult struct {
		stream *yamux.Stream
		err    error
	}

	resCh := make(chan streamResult, 1)
	go func() {
		stream, err := s.OpenStream()
		resCh <- streamResult{stream: stream, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, i18n.Errorf("agent %s stream failed: %w", name, res.err)
		}
		return res.stream, nil
	case <-time.After(15 * time.Second):
		return nil, i18n.Errorf("timeout opening stream to agent %s", name)
	}
}

// Dial is a convenience helper calling DefaultHub.Dial.
func Dial(ctx context.Context, name string) (net.Conn, error) {
	return DefaultHub.Dial(ctx, name)
}
