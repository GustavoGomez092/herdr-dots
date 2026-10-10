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
		Type      string     `json:"type"`
		Message   codexField `json:"message"`
		Name      codexField `json:"name"`
		Arguments codexField `json:"arguments"`
		Input     codexField `json:"input"`
	} `json:"payload"`
}

// codexField is a payload string. A value of another JSON type (an object,
// say) reads as "" instead of failing the whole record (T5).
type codexField string

func (f *codexField) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = codexField(s)
	}
	return nil
}

// rolloutStamp is the timestamp in rollout-<stamp>-<id>.jsonl.
var rolloutStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}$`)

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
	msg := string(p.Message)
	switch {
	case rec.Type == "event_msg" && p.Type == "user_message":
		if strings.TrimSpace(msg) == "" {
			return turns
		}
		return append(turns, Turn{Index: len(turns) + 1, User: msg, FromDots: FromDots(msg)})
	case len(turns) == 0:
		return turns
	case rec.Type == "event_msg" && p.Type == "agent_message":
		if msg == "" {
			return turns
		}
		cur := &turns[len(turns)-1]
		if cur.Assistant != "" {
			cur.Assistant += "\n"
		}
		cur.Assistant += msg
	case rec.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call"):
		cur := &turns[len(turns)-1]
		var paths []string
		for _, m := range patchFileRe.FindAllStringSubmatch(string(p.Arguments)+"\n"+string(p.Input), -1) {
			paths = append(paths, strings.TrimSpace(m[1]))
		}
		cur.Tools = append(cur.Tools, ToolUse{Name: string(p.Name), Paths: paths})
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

// codexSessionsDirs are the directories to search for a session, in order:
// $CODEX_HOME/sessions when CODEX_HOME is set (as Codex, mcpinstall and the
// codex backend resolve it), then <home>/.codex/sessions. The default comes
// second because this process's CODEX_HOME need not be the one the pane's
// Codex ran with.
func codexSessionsDirs(home string) []string {
	def := platform.CodexSessionsDir(home)
	if d := os.Getenv("CODEX_HOME"); d != "" {
		if own := filepath.Join(d, "sessions"); own != def {
			return []string{own, def}
		}
	}
	return []string{def}
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
		for _, dir := range codexSessionsDirs(home) {
			matches, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*", "rollout-*-"+value+".jsonl"))
			// The glob's * can span dashes ("abc-c1" ends in "-c1"): keep
			// only files whose whole tail after the timestamp is the id (T6).
			for i := len(matches) - 1; i >= 0; i-- {
				stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(matches[i]), "rollout-"), "-"+value+".jsonl")
				if rolloutStamp.MatchString(stamp) {
					return matches[i], nil
				}
			}
		}
		return "", fmt.Errorf("no transcript for codex session %s", value)
	}
	return "", fmt.Errorf("unknown session kind %q", kind)
}
