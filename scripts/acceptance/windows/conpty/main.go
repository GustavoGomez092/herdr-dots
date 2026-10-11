//go:build windows

// conptyh is the Windows stand-in for tmux in the interactive acceptance run
// (scripts/acceptance/windows/run-interactive.ps1). It is its own Go module,
// so the product's go.mod gains nothing.
//
// It starts a program (the real Herdr client) in a ConPTY, feeds everything
// the ConPTY writes into a VT emulator that keeps the screen buffer, and
// takes commands, one per line, on stdin and on a localhost TCP port (the
// tmux-like `cmd` client sends one command and prints the answer):
//
//	conptyh serve [--addr 127.0.0.1:47011] [--cols 200] [--rows 50] [--dir D] [--log F] [--key-delay 120ms] -- <program> [args...]
//	conptyh cmd [--addr 127.0.0.1:47011] <command line>
//
// Commands:
//
//	keys <tok> [tok...]      named keys (enter esc tab shift+tab space bs up down left right pgup pgdn ctrl+<x>)
//	                         or literal text; every key and every rune of text is its own write
//	type <text>              the rest of the line, one rune per write
//	burst <text>             the rest of the line in ONE write (a fast key burst)
//	raw <hex>                bytes in hex, one write
//	screen                   the emulated screen as text
//	save <file>              write the screen to a file (and print it)
//	wait-for <secs> <regex>  wait until the screen matches (prints the screen; TIMEOUT if not)
//	wait-gone <secs> <regex> wait until the screen no longer matches
//	resize <cols> <rows>     resize the ConPTY and the emulator
//	sleep <ms>
//	alive                    is the program still running
//	quit                     stop serving (the program is closed with the ConPTY)
//
// Every answer ends with a line "@@END OK", "@@END TIMEOUT" or "@@END ERR <why>".
package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/conpty"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/windows"
)

const endMark = "@@END "

type harness struct {
	mu       sync.Mutex // guards emu
	emu      *vt.Emulator
	pty      *conpty.ConPty
	keyDelay time.Duration
	log      *os.File
	logMu    sync.Mutex
	exited   chan struct{}
	exitCode uint32
	quit     chan struct{}
	quitOnce sync.Once
	cmdMu    sync.Mutex // one command at a time
}

func (h *harness) logf(format string, a ...any) {
	if h.log == nil {
		return
	}
	h.logMu.Lock()
	defer h.logMu.Unlock()
	fmt.Fprintf(h.log, "%s ", time.Now().UTC().Format("15:04:05.000"))
	fmt.Fprintf(h.log, format, a...)
	if !strings.HasSuffix(format, "\n") {
		fmt.Fprintln(h.log)
	}
}

// screen renders the emulator's visible screen as plain text, one line per
// row, trailing blanks trimmed. Wide characters take one rune (their second
// cell is empty and skipped).
func (h *harness) screen() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ht := h.emu.Width(), h.emu.Height()
	var b strings.Builder
	for y := 0; y < ht; y++ {
		var line strings.Builder
		for x := 0; x < w; x++ {
			c := h.emu.CellAt(x, y)
			if c == nil {
				line.WriteByte(' ')
				continue
			}
			if c.Width == 0 && c.Content == "" { // the tail of a wide character
				continue
			}
			if c.Content == "" {
				line.WriteByte(' ')
				continue
			}
			line.WriteString(c.Content)
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func (h *harness) write(p []byte) error {
	_, err := h.pty.Write(p)
	return err
}

var named = map[string]string{
	"enter": "\r", "return": "\r", "esc": "\x1b", "escape": "\x1b", "tab": "\t", "shift+tab": "\x1b[Z",
	"space": " ", "bs": "\x7f", "backspace": "\x7f", "up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C",
	"left": "\x1b[D", "pgup": "\x1b[5~", "pgdn": "\x1b[6~", "home": "\x1b[H", "end": "\x1b[F", "del": "\x1b[3~",
}

// keyBytes maps one token to the writes it stands for.
func keyBytes(tok string) [][]byte {
	if s, ok := named[strings.ToLower(tok)]; ok {
		return [][]byte{[]byte(s)}
	}
	lt := strings.ToLower(tok)
	if strings.HasPrefix(lt, "ctrl+") && len(lt) == 6 {
		c := lt[5]
		if c >= 'a' && c <= 'z' {
			return [][]byte{{c - 'a' + 1}}
		}
		switch c {
		case '@', ' ':
			return [][]byte{{0}}
		case '[':
			return [][]byte{{0x1b}}
		case '\\':
			return [][]byte{{0x1c}}
		case ']':
			return [][]byte{{0x1d}}
		}
	}
	var out [][]byte
	for _, r := range tok {
		out = append(out, []byte(string(r)))
	}
	return out
}

func (h *harness) sendWrites(ws [][]byte) error {
	for i, w := range ws {
		if i > 0 {
			time.Sleep(h.keyDelay)
		}
		if err := h.write(w); err != nil {
			return err
		}
	}
	time.Sleep(h.keyDelay)
	return nil
}

func (h *harness) waitScreen(secs float64, pattern string, want bool) (string, bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", false, err
	}
	deadline := time.Now().Add(time.Duration(secs * float64(time.Second)))
	for {
		s := h.screen()
		if re.MatchString(s) == want {
			return s, true, nil
		}
		if time.Now().After(deadline) {
			return s, false, nil
		}
		select {
		case <-h.exited:
			return h.screen(), false, fmt.Errorf("the program exited (code %d)", h.exitCode)
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// run executes one command line and returns its body and status.
func (h *harness) run(line string) (body, status string) {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	line = strings.TrimRight(line, "\r\n")
	cmd, rest, _ := strings.Cut(line, " ")
	h.logf("cmd %q", line)
	defer func() { h.logf("-> %s", status) }()
	switch cmd {
	case "":
		return "", "OK"
	case "keys":
		var ws [][]byte
		for _, tok := range strings.Fields(rest) {
			ws = append(ws, keyBytes(tok)...)
		}
		if err := h.sendWrites(ws); err != nil {
			return "", "ERR " + err.Error()
		}
		return "", "OK"
	case "type":
		var ws [][]byte
		for _, r := range rest {
			ws = append(ws, []byte(string(r)))
		}
		if err := h.sendWrites(ws); err != nil {
			return "", "ERR " + err.Error()
		}
		return "", "OK"
	case "burst":
		if err := h.sendWrites([][]byte{[]byte(rest)}); err != nil {
			return "", "ERR " + err.Error()
		}
		return "", "OK"
	case "raw":
		b, err := hex.DecodeString(strings.ReplaceAll(rest, " ", ""))
		if err != nil {
			return "", "ERR " + err.Error()
		}
		if err := h.sendWrites([][]byte{b}); err != nil {
			return "", "ERR " + err.Error()
		}
		return "", "OK"
	case "screen":
		return h.screen(), "OK"
	case "save":
		s := h.screen()
		if err := os.WriteFile(strings.TrimSpace(rest), []byte(s), 0o644); err != nil {
			return s, "ERR " + err.Error()
		}
		return s, "OK"
	case "wait-for", "wait-gone":
		secsS, pat, ok := strings.Cut(rest, " ")
		secs, err := strconv.ParseFloat(secsS, 64)
		if !ok || err != nil {
			return "", "ERR usage: " + cmd + " <secs> <regex>"
		}
		s, hit, err := h.waitScreen(secs, pat, cmd == "wait-for")
		if err != nil {
			return s, "ERR " + err.Error()
		}
		if !hit {
			return s, "TIMEOUT"
		}
		return s, "OK"
	case "resize":
		f := strings.Fields(rest)
		if len(f) != 2 {
			return "", "ERR usage: resize <cols> <rows>"
		}
		c, err1 := strconv.Atoi(f[0])
		r, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil || c < 2 || r < 2 {
			return "", "ERR bad size"
		}
		if err := h.pty.Resize(c, r); err != nil {
			return "", "ERR " + err.Error()
		}
		h.mu.Lock()
		h.emu.Resize(c, r)
		h.mu.Unlock()
		return fmt.Sprintf("resized to %dx%d", c, r), "OK"
	case "sleep":
		ms, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return "", "ERR " + err.Error()
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return "", "OK"
	case "alive":
		select {
		case <-h.exited:
			return fmt.Sprintf("exited %d", h.exitCode), "OK"
		default:
			return "running", "OK"
		}
	case "quit":
		h.quitOnce.Do(func() { close(h.quit) })
		return "bye", "OK"
	}
	return "", "ERR unknown command " + strconv.Quote(cmd)
}

func reply(w io.Writer, body, status string) {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	fmt.Fprintf(w, "%s%s%s\n", body, endMark, status)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:47011", "listen address for `cmd` clients")
	cols := fs.Int("cols", 200, "columns")
	rows := fs.Int("rows", 50, "rows")
	dir := fs.String("dir", "", "working directory of the program")
	logPath := fs.String("log", "", "log file (commands, raw output size, exits)")
	rawPath := fs.String("raw", "", "file to copy the raw ConPTY output into")
	keyDelay := fs.Duration("key-delay", 120*time.Millisecond, "pause after every key write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prog := fs.Args()
	if len(prog) == 0 {
		return errors.New("serve: no program given (serve [flags] -- program args...)")
	}

	h := &harness{keyDelay: *keyDelay, exited: make(chan struct{}), quit: make(chan struct{})}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			return err
		}
		defer f.Close()
		h.log = f
	}
	var raw io.Writer = io.Discard
	if *rawPath != "" {
		f, err := os.Create(*rawPath)
		if err != nil {
			return err
		}
		defer f.Close()
		raw = f
	}

	h.emu = vt.NewEmulator(*cols, *rows)
	pty, err := conpty.New(*cols, *rows, 0)
	if err != nil {
		return fmt.Errorf("conpty: %w", err)
	}
	h.pty = pty

	// Answers the emulator produces (device attributes, cursor reports...)
	// go back to the program, as a terminal's would.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := h.emu.Read(buf)
			if n > 0 {
				h.logf("emulator reply %q", buf[:n])
				_, _ = pty.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	// Everything the ConPTY draws goes into the emulator.
	go func() {
		buf := make([]byte, 64*1024)
		total := 0
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				total += n
				_, _ = raw.Write(buf[:n])
				h.mu.Lock()
				_, _ = h.emu.Write(buf[:n])
				h.mu.Unlock()
			}
			if err != nil {
				h.logf("conpty read ended after %d bytes: %v", total, err)
				return
			}
		}
	}()

	env := os.Environ()
	attr := &syscall.ProcAttr{Dir: *dir, Env: env}
	pid, handle, err := pty.Spawn(prog[0], prog, attr)
	if err != nil {
		return fmt.Errorf("spawn %s: %w", prog[0], err)
	}
	h.logf("spawned %q pid %d in %s, %dx%d", prog, pid, *dir, *cols, *rows)
	fmt.Printf("spawned pid %d\n", pid)
	go func() {
		_, _ = windows.WaitForSingleObject(windows.Handle(handle), windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(windows.Handle(handle), &code)
		h.exitCode = code
		h.logf("program exited with code %d", code)
		close(h.exited)
	}()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if strings.TrimSpace(line) != "" {
						body, status := h.run(line)
						reply(c, body, status)
					}
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()
	// The stdin line protocol, when stdin is a pipe or a file.
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			body, status := h.run(sc.Text())
			reply(os.Stdout, body, status)
		}
	}()

	<-h.quit
	h.logf("quit")
	go func() { _ = pty.Close() }()
	select {
	case <-h.exited:
	case <-time.After(5 * time.Second):
		_ = windows.TerminateProcess(windows.Handle(handle), 1)
	}
	return nil
}

func client(args []string) int {
	fs := flag.NewFlagSet("cmd", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:47011", "harness address")
	timeout := fs.Duration("timeout", 10*time.Minute, "give up after")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line := strings.Join(fs.Args(), " ")
	var c net.Conn
	var err error
	for i := 0; i < 50; i++ { // the server may still be starting
		c, err = net.DialTimeout("tcp", *addr, 2*time.Second)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "conptyh cmd:", err)
		return 2
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(*timeout))
	if _, err := fmt.Fprintln(c, line); err != nil {
		fmt.Fprintln(os.Stderr, "conptyh cmd:", err)
		return 2
	}
	r := bufio.NewReader(c)
	for {
		l, err := r.ReadString('\n')
		if strings.HasPrefix(l, endMark) {
			status := strings.TrimSpace(strings.TrimPrefix(l, endMark))
			if status == "OK" {
				return 0
			}
			fmt.Println(endMark + status)
			if status == "TIMEOUT" {
				return 1
			}
			return 3
		}
		os.Stdout.WriteString(l)
		if err != nil {
			fmt.Fprintln(os.Stderr, "conptyh cmd: connection ended:", err)
			return 2
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: conptyh serve [flags] -- program args... | conptyh cmd [--addr A] <command>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "conptyh:", err)
			os.Exit(1)
		}
	case "cmd":
		os.Exit(client(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", os.Args[1])
		os.Exit(2)
	}
}
