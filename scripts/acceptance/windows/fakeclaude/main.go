//go:build windows && acceptance

// fakeclaude is the stand-in Claude Code CLI for the Windows acceptance run
// (scripts/acceptance/windows/run.ps1). Built with -tags acceptance only.
//
// With -p it is a dot's review run: it logs its argv (and the start of the
// settings and system prompt files a .cmd shim is given) to claude-argv.log
// and prints a fixed verdict in the claude-cli JSON format
// (backend.ParseClaudeOutput).
//
// Without -p it is the coding agent in the Herdr pane: a process named
// claude.exe, which Herdr's Windows process scan identifies as Claude. It
// draws the screens the driver drops into <acc>\screen (Herdr's claude screen
// manifest: a spinner line means working, plain lines mean idle) and logs
// what is typed into it to agent-stdin.log.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func accDir() string {
	if d := os.Getenv("ACC_DIR"); d != "" {
		return d
	}
	return `C:\acc`
}

func appendFile(name, s string) {
	f, err := os.OpenFile(filepath.Join(accDir(), name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}

func oneLine(s string, n int) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "..."
	}
	return s
}

func readTrim(name, def string) string {
	b, err := os.ReadFile(filepath.Join(accDir(), name))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return def
	}
	return strings.TrimSpace(string(b))
}

func review(args []string) {
	wd, _ := os.Getwd()
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s pid=%d cwd=%s\n", time.Now().UTC().Format(time.RFC3339), os.Getpid(), wd)
	for i, a := range args {
		fmt.Fprintf(&b, "  argv[%d]=%s\n", i+1, oneLine(a, 300))
		if i > 0 {
			switch args[i-1] {
			case "--settings", "--append-system-prompt-file":
				if data, err := os.ReadFile(a); err == nil {
					fmt.Fprintf(&b, "    file(argv[%d])=%s\n", i+1, oneLine(string(data), 600))
				}
			}
		}
	}
	appendFile("claude-argv.log", b.String())
	in, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile(filepath.Join(accDir(), "claude-last-stdin.txt"), in, 0o644)

	msg := readTrim("fake-finding", `Hardcoded password in server.js: line 3 sets const PASSWORD = "hunter2". Anyone with repo access can log in. Move it to an environment variable.`)
	sev := readTrim("fake-severity", "critical")
	inner, _ := json.Marshal(struct {
		Relevant bool   `json:"relevant"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	}{true, sev, msg})
	type usage struct {
		In         int `json:"input_tokens"`
		Out        int `json:"output_tokens"`
		CacheRead  int `json:"cache_read_input_tokens"`
		CacheWrite int `json:"cache_creation_input_tokens"`
	}
	out, _ := json.Marshal(struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Usage   usage   `json:"usage"`
	}{"result", "success", false, string(inner), 0.0012, usage{100, 40, 0, 0}})
	fmt.Println(string(out))
}

const spinner = "✻ Cogitating… (1s · esc to interrupt)"

func agent() {
	fmt.Printf("fake claude agent pid=%d\n", os.Getpid())
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				s := string(buf[:n])
				appendFile("agent-stdin.log", fmt.Sprintf("%s %q\n", time.Now().UTC().Format("15:04:05.000"), s))
				if t := strings.TrimSpace(s); t != "" {
					fmt.Printf("> received: %s\n", oneLine(t, 400))
				}
			}
			if err != nil {
				appendFile("agent-stdin.log", fmt.Sprintf("stdin closed: %v\n", err))
				return
			}
		}
	}()
	dir := filepath.Join(accDir(), "screen")
	done := map[string]bool{}
	for {
		entries, _ := os.ReadDir(dir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && !done[e.Name()] {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			done[name] = true
			for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
				line = strings.TrimSpace(line)
				switch {
				case line == "working":
					fmt.Println(spinner)
				case strings.HasPrefix(line, "idle "):
					n := strings.TrimPrefix(line, "idle ")
					for i := 1; i <= 14; i++ {
						fmt.Printf("  (turn %s output %d)\n", n, i)
					}
				case strings.HasPrefix(line, "text "):
					fmt.Println(strings.TrimPrefix(line, "text "))
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "-p" || a == "--print" {
			review(args)
			return
		}
	}
	agent()
}
