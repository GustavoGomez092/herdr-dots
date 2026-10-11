//go:build windows && acceptance

// acctool drives the Herdr Dots engine IPC (a named pipe on Windows) the way
// the manage popup and the Dots pane do, and talks to Herdr's own socket API
// (also a named pipe) for the Windows acceptance run. Built with
// -tags acceptance only.
//
//	acctool parse <file>                      check a claude-cli JSON verdict
//	acctool <pipe> reload|snapshot
//	acctool <pipe> attach <pane> <slug...>
//	acctool <pipe> send <card-id> <parts>
//	acctool <pipe> dismiss <card-id>
//	acctool hapi <herdr-socket> <method> <params-json>
//	acctool sub <herdr-socket> <pane> <seconds>
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/platform"
)

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// herdrRequest sends one line to Herdr's socket API pipe and returns the
// connection's reader.
func herdrRequest(ctx context.Context, sock string, req any) (*bufio.Reader, func(), error) {
	conn, err := platform.DialPipe(ctx, platform.PipeName(sock))
	if err != nil {
		return nil, nil, err
	}
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return bufio.NewReaderSize(conn, 1<<22), func() { conn.Close() }, nil
}

func main() {
	if len(os.Args) < 3 {
		die(fmt.Errorf("usage: see the comment at the top of acctool/main.go"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch os.Args[1] {
	case "parse":
		b, err := os.ReadFile(os.Args[2])
		if err != nil {
			die(err)
		}
		res, err := backend.ParseClaudeOutput(b)
		if err != nil {
			die(err)
		}
		fmt.Printf("parsed verdict: %+v cost=%v known=%v\n", res.Verdict, res.CostUSD, res.CostKnown)
		return
	case "hapi":
		var params json.RawMessage = json.RawMessage(os.Args[4])
		r, closeFn, err := herdrRequest(ctx, os.Args[2], map[string]any{"id": "acc", "method": os.Args[3], "params": params})
		if err != nil {
			die(err)
		}
		defer closeFn()
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			die(err)
		}
		os.Stdout.Write(line)
		return
	case "sub":
		secs, _ := strconv.Atoi(os.Args[4])
		sctx, scancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
		defer scancel()
		req := map[string]any{"id": "sub", "method": "events.subscribe", "params": map[string]any{
			"subscriptions": []map[string]string{{"type": "pane.agent_status_changed", "pane_id": os.Args[3]}}}}
		r, closeFn, err := herdrRequest(sctx, os.Args[2], req)
		if err != nil {
			die(err)
		}
		go func() { <-sctx.Done(); closeFn() }()
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				fmt.Printf("%s %s", time.Now().UTC().Format("15:04:05.000"), line)
			}
			if err != nil {
				return
			}
		}
	}
	sock, cmd := os.Args[1], os.Args[2]
	c := ipc.Client{Path: sock}
	var err error
	switch cmd {
	case "reload":
		err = c.Call(ctx, ipc.CmdReloadDots, nil)
	case "attach":
		err = c.Call(ctx, ipc.CmdAttach, ipc.AttachArgs{PaneID: os.Args[3], Slugs: os.Args[4:]})
	case "send":
		n, _ := strconv.Atoi(os.Args[4])
		err = c.Call(ctx, ipc.CmdSend, ipc.SendArgs{IDs: []string{os.Args[3]}, Parts: map[string]int{os.Args[3]: n}})
	case "dismiss":
		err = c.Call(ctx, ipc.CmdDismiss, ipc.CardArgs{ID: os.Args[3]})
	case "snapshot":
		conn, derr := ipc.Dial(ctx, sock)
		if derr != nil {
			die(derr)
		}
		defer conn.Close()
		_ = json.NewEncoder(conn).Encode(ipc.Request{ID: 1, Cmd: ipc.CmdWatch})
		line, rerr := bufio.NewReaderSize(conn, 1<<22).ReadBytes('\n')
		if rerr != nil {
			die(rerr)
		}
		os.Stdout.Write(line)
		return
	default:
		die(fmt.Errorf("unknown cmd %s", cmd))
	}
	if err != nil {
		die(err)
	}
	fmt.Println("ok:", cmd)
}
