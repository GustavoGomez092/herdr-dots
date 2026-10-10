// Package herdrtest is an in-process fake of Herdr's socket API for tests. It
// implements only the methods herdr-dots uses.
package herdrtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
)

type Call struct {
	Method string
	Params map[string]any
}

type Server struct {
	Path string

	mu        sync.Mutex
	panes     map[string]herdr.PaneInfo
	reads     map[string]string
	calls     []Call
	noRecord  bool // DiscardCalls: long runs keep no call log
	subs      map[string][]*json.Encoder
	promptErr string
	ln        net.Listener
}

func New(t testing.TB) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "hdt") // short path: unix socket limit
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Path:  filepath.Join(dir, "h.sock"),
		panes: map[string]herdr.PaneInfo{},
		reads: map[string]string{},
		subs:  map[string][]*json.Encoder{},
	}
	s.ln, err = Listen(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	go s.accept()
	t.Cleanup(func() { s.ln.Close(); os.RemoveAll(dir) })
	return s
}

func (s *Server) SetPane(p herdr.PaneInfo) { s.mu.Lock(); s.panes[p.PaneID] = p; s.mu.Unlock() }
func (s *Server) RemovePane(id string)     { s.mu.Lock(); delete(s.panes, id); s.mu.Unlock() }
func (s *Server) SetRead(id, text string)  { s.mu.Lock(); s.reads[id] = text; s.mu.Unlock() }
func (s *Server) SetPromptError(code string) {
	s.mu.Lock()
	s.promptErr = code
	s.mu.Unlock()
}

// Push updates the pane's status and emits a pane.agent_status_changed event
// to every subscriber of that pane.
func (s *Server) Push(id string, st herdr.AgentStatus) {
	s.mu.Lock()
	p := s.panes[id]
	p.AgentStatus = st
	s.panes[id] = p
	encs := append([]*json.Encoder(nil), s.subs[id]...)
	s.mu.Unlock()
	for _, enc := range encs {
		_ = enc.Encode(map[string]any{
			"event": "pane.agent_status_changed",
			"data":  map[string]any{"type": "pane_agent_status_changed", "pane_id": id, "workspace_id": "w1", "agent_status": st, "agent": p.Agent},
		})
	}
}

// DiscardCalls stops recording calls, so a long run (the soak) doesn't grow
// the test's memory with every request it serves; Calls then returns nothing.
func (s *Server) DiscardCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noRecord, s.calls = true, nil
}

func (s *Server) Calls(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) WaitSubscribed(id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.subs[id])
		s.mu.Unlock()
		if n > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

type req struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func (s *Server) serve(c net.Conn) {
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	var r req
	if err := dec.Decode(&r); err != nil {
		c.Close()
		return
	}
	s.mu.Lock()
	if !s.noRecord {
		s.calls = append(s.calls, Call{Method: r.Method, Params: r.Params})
	}
	s.mu.Unlock()

	ok := func(result any) { _ = enc.Encode(map[string]any{"id": r.ID, "result": result}) }
	fail := func(code, msg string) {
		_ = enc.Encode(map[string]any{"id": r.ID, "error": map[string]string{"code": code, "message": msg}})
	}
	str := func(k string) string { v, _ := r.Params[k].(string); return v }

	if r.Method == "events.subscribe" {
		subs, _ := r.Params["subscriptions"].([]any)
		ok(map[string]any{"type": "subscription_started"})
		s.mu.Lock()
		for _, raw := range subs {
			m, _ := raw.(map[string]any)
			id, _ := m["pane_id"].(string)
			s.subs[id] = append(s.subs[id], enc)
		}
		s.mu.Unlock()
		_, _ = dec.Token() // block until the client closes the connection
		s.mu.Lock()
		for id, list := range s.subs {
			kept := list[:0]
			for _, e := range list {
				if e != enc {
					kept = append(kept, e)
				}
			}
			s.subs[id] = kept
		}
		s.mu.Unlock()
		c.Close()
		return
	}
	defer c.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case "ping":
		ok(map[string]any{"type": "pong", "version": "fake"})
	case "pane.get":
		p, found := s.panes[str("pane_id")]
		if !found {
			fail("pane_not_found", "pane not found") // what real Herdr 0.8.2 returns
			return
		}
		ok(map[string]any{"type": "pane_info", "pane": p})
	case "pane.list":
		list := []herdr.PaneInfo{}
		for _, p := range s.panes {
			list = append(list, p)
		}
		ok(map[string]any{"type": "pane_list", "panes": list})
	case "pane.read":
		ok(map[string]any{"type": "pane_read", "read": map[string]any{"text": s.reads[str("pane_id")]}})
	case "agent.prompt":
		if s.promptErr != "" {
			fail(s.promptErr, "prompt failed")
			return
		}
		ok(map[string]any{"type": "ok"})
	case "notification.show":
		ok(map[string]any{"type": "notification_show", "shown": true})
	case "pane.report_metadata", "plugin.pane.close":
		ok(map[string]any{"type": "ok"})
	case "plugin.pane.open":
		ok(map[string]any{"type": "plugin_pane_opened", "plugin_pane": map[string]any{"pane": map[string]any{"pane_id": "w1:dots"}}})
	default:
		fail("unknown_method", r.Method)
	}
}
