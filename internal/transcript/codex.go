package transcript

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"herdr-dots/internal/platform"
)

// Codex rollout files (~/.codex/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl).
// NOTE: built from Codex's documented format; not yet live-verified (see
// docs/superpowers/notes/2026-10-08-verification.md, V5).

type codexRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Type      string `json:"type"`
		Message   string `json:"message"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Input     string `json:"input"`
	} `json:"payload"`
}

var patchFileRe = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)

func ParseCodex(r io.Reader) ([]Turn, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var turns []Turn
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			turns = applyCodex(turns, line)
		}
		if errors.Is(err, io.EOF) {
			return turns, nil
		}
		if err != nil {
			return turns, err
		}
	}
}

func applyCodex(turns []Turn, line []byte) []Turn {
	var rec codexRecord
	if json.Unmarshal(line, &rec) != nil {
		return turns
	}
	p := rec.Payload
	switch {
	case rec.Type == "event_msg" && p.Type == "user_message":
		if strings.TrimSpace(p.Message) == "" {
			return turns
		}
		return append(turns, Turn{Index: len(turns) + 1, User: p.Message, FromDots: FromDots(p.Message)})
	case len(turns) == 0:
		return turns
	case rec.Type == "event_msg" && p.Type == "agent_message":
		cur := &turns[len(turns)-1]
		if cur.Assistant != "" {
			cur.Assistant += "\n"
		}
		cur.Assistant += p.Message
	case rec.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call"):
		cur := &turns[len(turns)-1]
		var paths []string
		for _, m := range patchFileRe.FindAllStringSubmatch(p.Arguments+"\n"+p.Input, -1) {
			paths = append(paths, strings.TrimSpace(m[1]))
		}
		cur.Tools = append(cur.Tools, ToolUse{Name: p.Name, Paths: paths})
	}
	return turns
}

type CodexReader struct{ Path string }

func (r CodexReader) Source() string { return SourceCodex }

func (r CodexReader) Turns(context.Context) ([]Turn, error) {
	f, err := os.Open(r.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseCodex(f)
}

func ResolveCodexPath(home, kind, value string) (string, error) {
	switch kind {
	case "path":
		if _, err := os.Stat(value); err != nil {
			return "", err
		}
		return value, nil
	case "id":
		if value == "" || strings.ContainsAny(value, `/\*?[]`) || strings.Contains(value, "..") {
			return "", fmt.Errorf("invalid codex session id %q", value)
		}
		matches, _ := filepath.Glob(filepath.Join(platform.CodexSessionsDir(home), "*", "*", "*", "rollout-*-"+value+".jsonl"))
		if len(matches) == 0 {
			return "", fmt.Errorf("no transcript for codex session %s", value)
		}
		return matches[len(matches)-1], nil
	}
	return "", fmt.Errorf("unknown session kind %q", kind)
}
