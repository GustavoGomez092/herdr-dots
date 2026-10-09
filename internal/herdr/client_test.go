package herdr_test

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
)

func client(s *herdrtest.Server) *herdr.SocketClient {
	return &herdr.SocketClient{Path: s.Path, PluginID: "herdr-dots"}
}

func TestPaneGetAndNotFound(t *testing.T) {
	s := herdrtest.New(t)
	s.SetPane(herdr.PaneInfo{PaneID: "w1:p1", Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Source: "herdr:claude", Agent: "claude", Kind: "id", Value: "abc"}})
	c := client(s)
	p, err := c.PaneGet(context.Background(), "w1:p1")
	if err != nil || p.Agent != "claude" || p.AgentSession.Value != "abc" || p.WorkDir() != "/repo" {
		t.Fatalf("PaneGet = %+v, %v", p, err)
	}
	_, err = c.PaneGet(context.Background(), "nope")
	if !herdr.IsNotFound(err) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestIsNotFoundMatchesHerdrCodes(t *testing.T) {
	for code, want := range map[string]bool{
		"not_found": true, "pane_not_found": true, "plugin_pane_not_found": true,
		"internal": false, "": false, "not_found_ish": false,
	} {
		if got := herdr.IsNotFound(&herdr.Error{Code: code}); got != want {
			t.Errorf("IsNotFound(%q) = %v, want %v", code, got, want)
		}
	}
	if herdr.IsNotFound(nil) {
		t.Error("nil is not not-found")
	}
}

func TestWorkDirPrefersForeground(t *testing.T) {
	p := herdr.PaneInfo{Cwd: "/a", ForegroundCwd: "/b"}
	if p.WorkDir() != "/b" {
		t.Fatal("foreground cwd should win")
	}
}

func TestPromptNotifyReportOpenClose(t *testing.T) {
	s := herdrtest.New(t)
	c := client(s)
	ctx := context.Background()
	if err := c.AgentPrompt(ctx, "w1:p1", "[◆ Security dot] hi"); err != nil {
		t.Fatal(err)
	}
	if got := s.Calls("agent.prompt")[0].Params["text"]; got != "[◆ Security dot] hi" {
		t.Fatalf("prompt text %v", got)
	}
	if err := c.Notify(ctx, "Security dot", "budget reached"); err != nil {
		t.Fatal(err)
	}
	if err := c.ReportPending(ctx, "w1:p1", "claude", 2); err != nil {
		t.Fatal(err)
	}
	if got := s.Calls("pane.report_metadata")[0].Params["display_agent"]; got != "claude ◆2" {
		t.Fatalf("display_agent %v", got)
	}
	_ = c.ReportPending(ctx, "w1:p1", "claude", 0)
	if got := s.Calls("pane.report_metadata")[1].Params["clear_display_agent"]; got != true {
		t.Fatalf("zero pending must clear, got %v", got)
	}
	id, err := c.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "pane", Placement: "split", Direction: "right", TargetPaneID: "w1:p1", Env: map[string]string{"HERDR_DOTS_TARGET": "w1:p1"}})
	if err != nil || id != "w1:dots" {
		t.Fatalf("open = %q %v", id, err)
	}
	open := s.Calls("plugin.pane.open")[0].Params
	if open["plugin_id"] != "herdr-dots" || open["target_pane_id"] != "w1:p1" {
		t.Fatalf("open params %v", open)
	}
	if err := c.ClosePluginPane(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestPaneRead(t *testing.T) {
	s := herdrtest.New(t)
	s.SetRead("w1:p1", "hello\nworld")
	got, err := client(s).PaneRead(context.Background(), "w1:p1", "recent_unwrapped", 200)
	if err != nil || got != "hello\nworld" {
		t.Fatalf("read %q %v", got, err)
	}
	p := s.Calls("pane.read")[0].Params
	if p["source"] != "recent_unwrapped" || p["strip_ansi"] != true {
		t.Fatalf("read params %v", p)
	}
}

func TestSubscribeStreamsStatusAndStopsOnCancel(t *testing.T) {
	s := herdrtest.New(t)
	s.SetPane(herdr.PaneInfo{PaneID: "w1:p1", Agent: "claude"})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := client(s).Subscribe(ctx, "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	if !s.WaitSubscribed("w1:p1", time.Second) {
		t.Fatal("not subscribed")
	}
	s.Push("w1:p1", herdr.StatusWorking)
	s.Push("w1:p1", herdr.StatusIdle)
	for _, want := range []herdr.AgentStatus{herdr.StatusWorking, herdr.StatusIdle} {
		select {
		case ev := <-ch:
			if ev.Status != want || ev.PaneID != "w1:p1" {
				t.Fatalf("event %+v, want %s", ev, want)
			}
		case <-time.After(time.Second):
			t.Fatal("no event")
		}
	}
	cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel should close after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
}

func TestParseInvocationContext(t *testing.T) {
	ic, err := herdr.ParseInvocationContext(`{"focused_pane_id":"w1:p2","focused_pane_agent":"codex","focused_pane_cwd":"/r","tab_id":"w1:t1","workspace_id":"w1"}`)
	if err != nil || ic.FocusedPaneID != "w1:p2" || ic.FocusedPaneAgent != "codex" {
		t.Fatalf("%+v %v", ic, err)
	}
	if ic, err := herdr.ParseInvocationContext(""); err != nil || ic.FocusedPaneID != "" {
		t.Fatal("empty context should be zero value, no error")
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	if _, err := herdr.NewFromEnv(); err == nil {
		t.Fatal("want error without socket")
	}
	t.Setenv("HERDR_SOCKET_PATH", "/x.sock")
	t.Setenv("HERDR_PLUGIN_ID", "herdr-dots")
	c, err := herdr.NewFromEnv()
	if err != nil || c.Path != "/x.sock" || c.PluginID != "herdr-dots" {
		t.Fatalf("%+v %v", c, err)
	}
}

// rawListener returns a Herdr socket path whose connections are handed to h.
func rawListener(t *testing.T, h func(net.Conn)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hdt")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "h.sock")
	ln, err := herdrtest.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close(); os.RemoveAll(dir) })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go h(c)
		}
	}()
	return path
}

func TestSubscribeAckTimeout(t *testing.T) {
	path := rawListener(t, func(c net.Conn) {
		defer c.Close()
		_, _ = bufio.NewReader(c).ReadBytes('\n')
		time.Sleep(3 * time.Second) // never replies
	})
	c := &herdr.SocketClient{Path: path, Timeout: 100 * time.Millisecond}
	start := time.Now()
	if _, err := c.Subscribe(context.Background(), "w1:p1"); err == nil {
		t.Fatal("want error when ack never arrives")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("ack wait not bounded: %v", time.Since(start))
	}
}

func TestSubscribeAckCancel(t *testing.T) {
	path := rawListener(t, func(c net.Conn) {
		defer c.Close()
		_, _ = bufio.NewReader(c).ReadBytes('\n')
		time.Sleep(3 * time.Second)
	})
	c := &herdr.SocketClient{Path: path, Timeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Subscribe(ctx, "w1:p1"); done <- err }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want error after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after ctx cancel")
	}
}

func TestSubscribeChannelClosesWhenServerDrops(t *testing.T) {
	path := rawListener(t, func(c net.Conn) {
		_, _ = bufio.NewReader(c).ReadBytes('\n')
		_, _ = c.Write([]byte(`{"id":"x","result":{"type":"subscription_started"}}` + "\n"))
		time.Sleep(50 * time.Millisecond)
		c.Close()
	})
	c := &herdr.SocketClient{Path: path}
	ch, err := c.Subscribe(context.Background(), "w1:p1") // ctx stays live
	if err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after server dropped connection")
	}
}
