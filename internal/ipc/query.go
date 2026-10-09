package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"time"
)

// Query commands (Phase 5) return data in Response.Data. Each one runs on its
// own connection: the server answers once and closes it, and a client that
// hangs up cancels the query's context.
const (
	CmdListDots   = "list_dots"   // PaneArgs → DotsResult
	CmdAsk        = "ask"         // AskArgs → AskResult
	CmdAskCouncil = "ask_council" // AskCouncilArgs → CouncilResult
	CmdListQueue  = "list_queue"  // ListQueueArgs → QueueResult
)

// QueryCmds are the commands serveConn hands to a Querier instead of Handle.
var QueryCmds = []string{CmdListDots, CmdAsk, CmdAskCouncil, CmdListQueue}

// Querier is implemented by handlers that answer query commands with data.
type Querier interface {
	Query(ctx context.Context, cmd string, args json.RawMessage) (any, error)
}

// ErrQueryUnsupported is the error text a handler without Query returns.
var ErrQueryUnsupported = errors.New("query not supported")

// Bounds on what a query reply may cost the engine: a result larger than
// maxQueryResponse becomes an error, and a client that does not read its
// answer within queryWriteTimeout is dropped. Variables so tests can shrink them.
var (
	maxQueryResponse  = 1 << 20
	queryWriteTimeout = 10 * time.Second
)

// ListQueueArgs.Status values.
const (
	QueuePending = "pending" // pending and auto cards (the default)
	QueueAll     = "all"     // every card of the pane, newest first
)

type PaneArgs struct {
	PaneID string `json:"pane_id"`
}

type AskArgs struct {
	PaneID       string `json:"pane_id"`
	Slug         string `json:"slug"`
	Question     string `json:"question"`
	IncludeTurns bool   `json:"include_turns"`
}

type AskCouncilArgs struct {
	PaneID   string `json:"pane_id"`
	Question string `json:"question"`
}

type ListQueueArgs struct {
	PaneID string `json:"pane_id"`
	Status string `json:"status,omitempty"`
}

// DotInfo is one attached dot as the MCP server's list_dots shows it.
type DotInfo struct {
	Slug           string  `json:"slug"`
	Name           string  `json:"name"`
	PurposeSummary string  `json:"purpose_summary"`
	Mode           string  `json:"mode"`
	Status         string  `json:"status"`
	Paused         bool    `json:"paused"`
	Calls          int     `json:"calls"`
	Budget         int     `json:"budget"`
	CostUSD        float64 `json:"cost_usd"`
}

// DotsResult is the pane's project and its attached dots.
type DotsResult struct {
	ProjectID   string    `json:"project_id,omitempty"`
	ProjectName string    `json:"project_name,omitempty"`
	Dots        []DotInfo `json:"dots"`
}

// AskResult is one dot's answer to ask_dot.
type AskResult struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Relevant  bool    `json:"relevant"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`
	Model     string  `json:"model,omitempty"`
}

type CouncilMember struct {
	Slug     string `json:"slug"`
	Severity string `json:"severity,omitempty"`
	Stance   string `json:"stance,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
}

// CouncilResult is ask_council's merged digest. CostUSD is approximate: the
// pane's spend while the council ran.
type CouncilResult struct {
	Digest  string          `json:"digest"`
	Members []CouncilMember `json:"members"`
	CostUSD float64         `json:"cost_usd"`
	Note    string          `json:"note,omitempty"`
}

type CardInfo struct {
	ID       string `json:"id"`
	Dot      string `json:"dot"`
	Severity string `json:"severity"`
	Turn     int    `json:"turn"`
	Status   string `json:"status"`
	Body     string `json:"body"`
}

// QueueResult holds at most 20 cards; Total counts every matching card.
type QueueResult struct {
	Cards []CardInfo `json:"cards"`
	Total int        `json:"total"`
}

func isQuery(cmd string) bool { return slices.Contains(QueryCmds, cmd) }

// serveQuery answers one query; serveConn closes the connection afterwards.
// The query's context ends when the client hangs up, so the engine stops
// working for an agent that gave up. The reply is size-bounded and written
// under a deadline, so a client that never reads cannot hold the goroutine.
func serveQuery(ctx context.Context, conn net.Conn, dec *json.Decoder, enc *json.Encoder, req Request, h Handler) {
	resp := Response{ID: req.ID, OK: true}
	q, ok := h.(Querier)
	if !ok {
		resp.OK, resp.Error = false, ErrQueryUnsupported.Error()
		_ = conn.SetWriteDeadline(time.Now().Add(queryWriteTimeout))
		_ = enc.Encode(resp)
		return
	}
	qctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = io.Copy(io.Discard, io.MultiReader(dec.Buffered(), conn))
		cancel() // EOF or a closed connection: nobody waits for the answer
	}()
	v, err := q.Query(qctx, req.Cmd, req.Args)
	if err == nil {
		resp.Data, err = json.Marshal(v)
		if err == nil && len(resp.Data) > maxQueryResponse {
			err = fmt.Errorf("result too large (%d bytes, limit %d)", len(resp.Data), maxQueryResponse)
		}
	}
	if err != nil {
		resp.OK, resp.Error, resp.Data = false, err.Error(), nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(queryWriteTimeout))
	_ = enc.Encode(resp)
}

// Query sends one query command and decodes Response.Data into out (when out
// is non-nil). Unlike Call it has no 30-second default: the context's
// deadline, if any, bounds the exchange, and cancelling ctx aborts it.
func (c Client) Query(ctx context.Context, cmd string, args, out any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	var raw json.RawMessage
	if args != nil {
		if raw, err = json.Marshal(args); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: cmd, Args: raw}); err != nil {
		return ctxErr(ctx, err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return ctxErr(ctx, err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

// ctxErr prefers the context's error over the i/o error it caused.
func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return context.DeadlineExceeded
		}
	}
	return err
}
