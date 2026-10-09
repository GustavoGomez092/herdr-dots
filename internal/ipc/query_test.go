package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

// queryHandler answers queries with a scripted function. v1 commands go to
// the embedded fakeHandler; the command "slow" sleeps for slow first.
type queryHandler struct {
	*fakeHandler
	query func(ctx context.Context, cmd string, args json.RawMessage) (any, error)
	slow  time.Duration
}

func newQueryHandler(f func(ctx context.Context, cmd string, args json.RawMessage) (any, error)) *queryHandler {
	return &queryHandler{fakeHandler: &fakeHandler{updates: make(chan view.Snapshot, 1)}, query: f}
}

func (q *queryHandler) Handle(ctx context.Context, cmd string, args json.RawMessage) error {
	if cmd == "slow" {
		time.Sleep(q.slow)
	}
	return q.fakeHandler.Handle(ctx, cmd, args)
}

func (q *queryHandler) Query(ctx context.Context, cmd string, args json.RawMessage) (any, error) {
	return q.query(ctx, cmd, args)
}

// serveHandler serves h on a short socket path (macOS caps unix socket paths
// at 104 bytes, and t.TempDir() is long there).
func serveHandler(t *testing.T, h Handler) Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipcq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "q.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go Serve(ctx, ln, h)
	return Client{Path: path}
}

func TestQueryRoundTripCarriesData(t *testing.T) {
	var got AskArgs
	c := serveHandler(t, newQueryHandler(func(_ context.Context, cmd string, args json.RawMessage) (any, error) {
		if cmd != CmdAsk {
			return nil, errors.New("wrong command " + cmd)
		}
		if err := json.Unmarshal(args, &got); err != nil {
			return nil, err
		}
		return AskResult{Slug: got.Slug, Name: "Security", Relevant: true, Severity: "warn",
			Message: "use httpOnly cookies", CostUSD: 0.01, CostKnown: true, Model: "sonnet"}, nil
	}))
	var res AskResult
	err := c.Query(context.Background(), CmdAsk, AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if got != (AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}) {
		t.Fatalf("server got %+v", got)
	}
	if res.Name != "Security" || res.Severity != "warn" || res.Message != "use httpOnly cookies" || !res.CostKnown || res.Model != "sonnet" {
		t.Fatalf("client got %+v", res)
	}
}

func TestQueryErrorsComeBackAsText(t *testing.T) {
	c := serveHandler(t, newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		return nil, errors.New("ask limit reached; continue without the dot")
	}))
	err := c.Query(context.Background(), CmdListDots, PaneArgs{PaneID: "w1:p1"}, &DotsResult{})
	if err == nil || err.Error() != "ask limit reached; continue without the dot" {
		t.Fatalf("got %v", err)
	}
}

func TestQueryWithoutQuerierIsRefused(t *testing.T) {
	h, c := serve(t) // v1's fakeHandler has no Query method
	err := c.Query(context.Background(), CmdListQueue, ListQueueArgs{PaneID: "w1:p1"}, &QueueResult{})
	if err == nil || err.Error() != ErrQueryUnsupported.Error() {
		t.Fatalf("got %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.got) != 0 {
		t.Fatalf("a query must never reach Handle: %v", h.got)
	}
}

func TestV1ResponsesCarryNoData(t *testing.T) {
	_, c := serve(t)
	conn, err := net.Dial("unix", c.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Request{ID: 7, Cmd: CmdPing}); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, `"data"`) || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("v1 response %s", line)
	}
	// A v1 client's Response type (no Data field) still decodes a query reply.
	var v1 struct {
		ID       int            `json:"id"`
		OK       bool           `json:"ok"`
		Error    string         `json:"error"`
		Snapshot *view.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(`{"id":1,"ok":true,"data":{"dots":[]}}`), &v1); err != nil || !v1.OK {
		t.Fatalf("v1 decode: %v %+v", err, v1)
	}
}

// Review Focus 4: a dot run longer than Call's 30-second default must not be
// cut off; Query follows its context instead (the default is shortened here).
func TestQueryFollowsTheContextNotCallsDefault(t *testing.T) {
	old := callTimeout
	callTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callTimeout = old })
	h := newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		time.Sleep(200 * time.Millisecond) // a "run" four times longer than the default
		return DotsResult{ProjectID: "g-0123456789abcdef", Dots: []DotInfo{{Slug: "security"}}}, nil
	})
	h.slow = 200 * time.Millisecond
	c := serveHandler(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var res DotsResult
	if err := c.Query(ctx, CmdListDots, PaneArgs{PaneID: "w1:p1"}, &res); err != nil {
		t.Fatalf("query cut short: %v", err)
	}
	if res.ProjectID != "g-0123456789abcdef" || len(res.Dots) != 1 {
		t.Fatalf("res %+v", res)
	}
	if err := c.Call(context.Background(), "slow", nil); err == nil {
		t.Fatal("Call must keep its default deadline")
	}
}

func TestQueryHonorsItsOwnDeadline(t *testing.T) {
	c := serveHandler(t, newQueryHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Query(ctx, CmdAsk, AskArgs{PaneID: "w1:p1", Slug: "security", Question: "q"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("deadline ignored")
	}
}

func TestQueryContextEndsWhenTheClientHangsUp(t *testing.T) {
	ended := make(chan struct{})
	c := serveHandler(t, newQueryHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := c.Query(ctx, CmdAsk, AskArgs{PaneID: "w1:p1"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine kept working for a client that left")
	}
}

func TestOversizedQueryResultBecomesAnError(t *testing.T) {
	old := maxQueryResponse
	maxQueryResponse = 1000
	t.Cleanup(func() { maxQueryResponse = old })
	c := serveHandler(t, newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		return AskResult{Message: strings.Repeat("x", 5000)}, nil
	}))
	err := c.Query(context.Background(), CmdAsk, AskArgs{PaneID: "w1:p1"}, &AskResult{})
	if err == nil || !strings.Contains(err.Error(), "result too large") {
		t.Fatalf("got %v", err)
	}
}

// A client that sends a query and never reads must not hold the server forever.
func TestQueryReplyToANonReadingClientTimesOut(t *testing.T) {
	old := queryWriteTimeout
	queryWriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { queryWriteTimeout = old })
	srv, cli := net.Pipe() // unbuffered: a write blocks until the peer reads
	defer cli.Close()
	h := newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		return DotsResult{}, nil
	})
	done := make(chan struct{})
	go func() {
		defer srv.Close()
		serveQuery(context.Background(), srv, json.NewDecoder(srv), json.NewEncoder(srv), Request{ID: 1, Cmd: CmdListDots}, h)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveQuery blocked on a client that never reads")
	}
}

// Unknown fields in a request are ignored, so newer peers interoperate.
func TestQueryToleratesUnknownFields(t *testing.T) {
	c := serveHandler(t, newQueryHandler(func(_ context.Context, _ string, args json.RawMessage) (any, error) {
		var a PaneArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		return map[string]any{"project_id": a.PaneID, "dots": []any{}, "future_field": 1}, nil
	}))
	conn, err := net.Dial("unix", c.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"id":3,"cmd":"list_dots","args":{"pane_id":"w1:p1","extra":true},"future":"x"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil || !resp.OK || resp.ID != 3 {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	var res DotsResult
	if err := json.Unmarshal(resp.Data, &res); err != nil || res.ProjectID != "w1:p1" {
		t.Fatalf("data %s err %v", resp.Data, err)
	}
}
