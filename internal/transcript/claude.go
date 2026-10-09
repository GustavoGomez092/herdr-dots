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
	"strings"

	"herdr-dots/internal/platform"
)

type claudeRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ParseClaude reads Claude Code session JSONL. Malformed lines (including a
// half-written last line) are skipped; lines of any size are supported.
func ParseClaude(r io.Reader) ([]Turn, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var turns []Turn
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			turns = applyClaude(turns, line)
		}
		if errors.Is(err, io.EOF) {
			return turns, nil
		}
		if err != nil {
			return turns, err
		}
	}
}

func applyClaude(turns []Turn, line []byte) []Turn {
	var rec claudeRecord
	if json.Unmarshal(line, &rec) != nil || rec.IsSidechain || rec.IsMeta {
		return turns
	}
	switch rec.Type {
	case "user":
		text, ok := userPrompt(rec.Message.Content)
		if !ok {
			return turns
		}
		return append(turns, Turn{Index: len(turns) + 1, User: text, FromDots: FromDots(text)})
	case "assistant":
		if len(turns) == 0 {
			return turns
		}
		cur := &turns[len(turns)-1]
		for _, b := range blocks(rec.Message.Content) {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				if cur.Assistant != "" {
					cur.Assistant += "\n"
				}
				cur.Assistant += b.Text
			case "tool_use":
				cur.Tools = append(cur.Tools, ToolUse{Name: b.Name, Paths: inputPaths(b.Input)})
			}
		}
	}
	return turns
}

// userPrompt returns the text of a real prompt. Tool results and interrupt
// markers are not prompts.
func userPrompt(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s) == "" {
			return "", false
		}
		return s, true
	}
	var parts []string
	for _, b := range blocks(raw) {
		switch b.Type {
		case "tool_result":
			return "", false
		case "text":
			if strings.HasPrefix(b.Text, "[Request interrupted") {
				return "", false
			}
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func blocks(raw json.RawMessage) []claudeBlock {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return []claudeBlock{{Type: "text", Text: s}}
	}
	var bs []claudeBlock
	_ = json.Unmarshal(raw, &bs)
	return bs
}

func inputPaths(in map[string]any) []string {
	var out []string
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if s, ok := in[k].(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

type ClaudeReader struct{ Path string }

func (r ClaudeReader) Source() string { return SourceClaude }

func (r ClaudeReader) Turns(context.Context) ([]Turn, error) {
	f, err := os.Open(r.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseClaude(f)
}

// ResolveClaudePath finds the transcript for a Herdr-reported session: a
// direct path, or <home>/.claude/projects/*/<id>.jsonl. The glob covers every
// project folder, so it doesn't depend on how Claude Code encodes a cwd
// (C:\Users\x\repo becomes C--Users-x-repo on Windows).
func ResolveClaudePath(home, kind, value string) (string, error) {
	switch kind {
	case "path":
		if _, err := os.Stat(value); err != nil {
			return "", err
		}
		return value, nil
	case "id":
		if value == "" || strings.ContainsAny(value, `/\*?[]`) || strings.Contains(value, "..") {
			return "", fmt.Errorf("invalid claude session id %q", value)
		}
		matches, _ := filepath.Glob(filepath.Join(platform.ClaudeProjectsDir(home), "*", value+".jsonl"))
		if len(matches) == 0 {
			return "", fmt.Errorf("no transcript for claude session %s", value)
		}
		return matches[0], nil
	}
	return "", fmt.Errorf("unknown session kind %q", kind)
}
