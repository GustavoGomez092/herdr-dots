package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

type Client interface {
	Ping(ctx context.Context) error
	PaneGet(ctx context.Context, paneID string) (PaneInfo, error)
	PaneList(ctx context.Context) ([]PaneInfo, error)
	PaneRead(ctx context.Context, paneID, source string, lines int) (string, error)
	AgentPrompt(ctx context.Context, target, text string) error
	Notify(ctx context.Context, title, body string) error
	ReportPending(ctx context.Context, paneID, agent string, pending int) error
	OpenPluginPane(ctx context.Context, req OpenPane) (string, error)
	ClosePluginPane(ctx context.Context, paneID string) error
	Subscribe(ctx context.Context, paneID string) (<-chan StatusEvent, error)
}

// SocketClient opens one connection per request (as herdr-plus does) and one
// long-lived connection per subscription.
type SocketClient struct {
	Path     string
	PluginID string
	Timeout  time.Duration // per request; 0 → 10s
}

var _ Client = (*SocketClient)(nil)

func NewFromEnv() (*SocketClient, error) {
	p := os.Getenv("HERDR_SOCKET_PATH")
	if p == "" {
		return nil, errors.New("HERDR_SOCKET_PATH is not set; are you running inside herdr?")
	}
	id := os.Getenv("HERDR_PLUGIN_ID")
	if id == "" {
		id = "herdr-dots"
	}
	return &SocketClient{Path: p, PluginID: id}, nil
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

func (c *SocketClient) dial(ctx context.Context) (net.Conn, error) {
	conn, err := dialSocket(ctx, c.Path)
	if err != nil {
		return nil, fmt.Errorf("connect herdr socket: %w", err)
	}
	return conn, nil
}

func (c *SocketClient) call(ctx context.Context, method string, params any, out any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if params == nil {
		params = map[string]any{}
	}
	if err := json.NewEncoder(conn).Encode(request{ID: "herdr-dots", Method: method, Params: params}); err != nil {
		return fmt.Errorf("%s: write: %w", method, err)
	}
	var resp response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return fmt.Errorf("%s: read: %w", method, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("%s: decode: %w", method, err)
		}
	}
	return nil
}

func (c *SocketClient) Ping(ctx context.Context) error { return c.call(ctx, "ping", nil, nil) }

func (c *SocketClient) PaneGet(ctx context.Context, paneID string) (PaneInfo, error) {
	var out struct {
		Pane PaneInfo `json:"pane"`
	}
	err := c.call(ctx, "pane.get", map[string]any{"pane_id": paneID}, &out)
	return out.Pane, err
}

func (c *SocketClient) PaneList(ctx context.Context) ([]PaneInfo, error) {
	var out struct {
		Panes []PaneInfo `json:"panes"`
	}
	err := c.call(ctx, "pane.list", map[string]any{}, &out)
	return out.Panes, err
}

func (c *SocketClient) PaneRead(ctx context.Context, paneID, source string, lines int) (string, error) {
	params := map[string]any{"pane_id": paneID, "source": source, "format": "text", "strip_ansi": true}
	if lines > 0 {
		params["lines"] = lines
	}
	var out struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err := c.call(ctx, "pane.read", params, &out)
	return out.Read.Text, err
}

func (c *SocketClient) AgentPrompt(ctx context.Context, target, text string) error {
	return c.call(ctx, "agent.prompt", map[string]any{"target": target, "text": text}, nil)
}

func (c *SocketClient) Notify(ctx context.Context, title, body string) error {
	return c.call(ctx, "notification.show", map[string]any{"title": title, "body": body, "sound": "none"}, nil)
}

// ReportPending shows "<agent> ◆N" as the pane's agent label in Herdr's
// sidebar, and clears it when nothing is pending.
func (c *SocketClient) ReportPending(ctx context.Context, paneID, agent string, pending int) error {
	params := map[string]any{"pane_id": paneID, "source": "herdr-dots"}
	if pending > 0 {
		params["display_agent"] = fmt.Sprintf("%s ◆%d", agent, pending)
	} else {
		params["clear_display_agent"] = true
	}
	return c.call(ctx, "pane.report_metadata", params, nil)
}

func (c *SocketClient) OpenPluginPane(ctx context.Context, r OpenPane) (string, error) {
	params := map[string]any{"plugin_id": c.PluginID, "entrypoint": r.Entrypoint, "focus": r.Focus}
	if r.Placement != "" {
		params["placement"] = r.Placement
	}
	if r.Direction != "" {
		params["direction"] = r.Direction
	}
	if r.TargetPaneID != "" {
		params["target_pane_id"] = r.TargetPaneID
	}
	if r.Width != nil {
		params["width"] = r.Width
	}
	if r.Height != nil {
		params["height"] = r.Height
	}
	if len(r.Env) > 0 {
		params["env"] = r.Env
	}
	var out struct {
		PluginPane struct {
			Pane PaneInfo `json:"pane"`
		} `json:"plugin_pane"`
	}
	err := c.call(ctx, "plugin.pane.open", params, &out)
	return out.PluginPane.Pane.PaneID, err
}

func (c *SocketClient) ClosePluginPane(ctx context.Context, paneID string) error {
	return c.call(ctx, "plugin.pane.close", map[string]any{"pane_id": paneID}, nil)
}

// Subscribe streams pane.agent_status_changed events for one pane until ctx is
// cancelled or the connection drops; the channel is then closed.
func (c *SocketClient) Subscribe(ctx context.Context, paneID string) (<-chan StatusEvent, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	// done is closed when the reader goroutine exits, so the ctx watcher never
	// outlives the connection. The watcher also covers the ack wait below.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	fail := func(err error) (<-chan StatusEvent, error) {
		close(done)
		conn.Close()
		return nil, err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	sub := map[string]any{"subscriptions": []map[string]any{{"type": "pane.agent_status_changed", "pane_id": paneID}}}
	if err := json.NewEncoder(conn).Encode(request{ID: "herdr-dots-sub", Method: "events.subscribe", Params: sub}); err != nil {
		return fail(err)
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	var first response
	if err := dec.Decode(&first); err != nil {
		return fail(fmt.Errorf("events.subscribe: %w", err))
	}
	if first.Error != nil {
		return fail(first.Error)
	}
	_ = conn.SetDeadline(time.Time{}) // streaming has no deadline
	ch := make(chan StatusEvent, 16)
	go func() {
		defer close(ch)
		defer close(done)
		defer conn.Close()
		for {
			var env struct {
				Event string `json:"event"`
				Data  struct {
					PaneID      string      `json:"pane_id"`
					AgentStatus AgentStatus `json:"agent_status"`
					Agent       string      `json:"agent"`
				} `json:"data"`
			}
			if err := dec.Decode(&env); err != nil {
				return
			}
			if env.Event != "pane.agent_status_changed" {
				continue
			}
			select {
			case ch <- StatusEvent{PaneID: env.Data.PaneID, Status: env.Data.AgentStatus, Agent: env.Data.Agent}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
