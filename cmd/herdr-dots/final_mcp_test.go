package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/paths"
)

// Phase 5 final review M10: stray arguments after the flags are refused
// (a typo'd client config must not run with half its intent).
func TestServeMCPRejectsPositionalArguments(t *testing.T) {
	_, _, args := mcpDirs(t)
	err := serveMCP(context.Background(), append(args, "extra"), mcpEnvOf(nil), &mcp.StdioTransport{}, mcpNoEnsure(t))
	if err == nil || !strings.Contains(err.Error(), `unexpected argument "extra"`) {
		t.Fatalf("got %v", err)
	}
}

// Phase 5 final review M10: a log that can't be opened says so once on
// stderr (safe on stdio: only stdout carries the protocol).
func TestMCPLoggerSaysOnStderrWhenTheLogCannotBeOpened(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "state")
	if err := os.WriteFile(blocked, []byte("a file where the state dir should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger, closeLog := mcpLogger(paths.New(filepath.Join(root, "cfg"), blocked, ""), &stderr)
	defer closeLog()
	logger.Printf("dropped")
	got := stderr.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "herdr-dots mcp: can't open the log") || !strings.Contains(got, blocked) {
		t.Fatalf("stderr %q", got)
	}
}
