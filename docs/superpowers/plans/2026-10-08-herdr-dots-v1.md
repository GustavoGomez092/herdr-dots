# Herdr Dots v1 (Core Loop) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first increment (a scaffold, not a release) of Herdr Dots: a Herdr plugin in which specialist "dots" attach to a Claude Code or Codex pane, review each finished turn, and contribute through a Dots side pane with an approval queue.

**Architecture:** A single Go binary, `herdr-dots`, with subcommands. `engine` is a detached background process (one per Herdr session) that owns all live state, subscribes to Herdr's socket events, reads agent transcripts, runs dots through headless CLI backends, and delivers messages. `pane`, `manage` and `summon-ui` are Bubble Tea TUIs that Herdr opens in its panes; they are thin clients of the engine over a local Unix socket. `action` subcommands are bound to Herdr plugin actions.

**Tech Stack:** Go 1.25, `github.com/BurntSushi/toml`, `github.com/charmbracelet/bubbletea` v1, `bubbles`, `lipgloss`, `github.com/bmatcuk/doublestar/v4`. Herdr 0.8.x socket API (newline-delimited JSON over a Unix socket).

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-design.md`

**Deviations from the spec (deliberate, small):** `Turn.FromDot` becomes `Turn.FromDots []string` (one delivery can batch several dots); the engine uses `pane.get` instead of `agent.get` (same fields, includes `agent_session`); UI tests drive Bubble Tea models directly through `Update`/`View` instead of `teatest` (same coverage, no extra dependency); a malformed verdict counts toward a dot's 3-failure stop.

## Global Constraints

- v1 is a **scaffold**. Never describe the project as done, finished or ready in code, commits, docs or reports. Version stays `0.1.0` (`0.x`).
- **Dot memory is VITAL for Phase 2.** The README roadmap must say so verbatim.
- Plugin id `herdr-dots`; action ids `herdr-dots.manage`, `herdr-dots.toggle-pane`, `herdr-dots.summon`, `herdr-dots.pause-all`.
- Go module path: `herdr-dots`. Go version directive: `go 1.25`.
- Platforms: macOS and Linux only (`platforms = ["macos", "linux"]`). No Windows code paths.
- Dots are read-only: claude backend uses only `Read,Grep,Glob` (or no tools); codex uses `--sandbox read-only`. Never pass `--dangerously-skip-permissions` or `bypassPermissions`.
- Every child process environment is stripped of `HERDR_*`, `CLAUDECODE` and `CLAUDE_CODE_ENTRYPOINT`.
- Claude backend always passes `--settings '{"disableAllHooks":true}'` and `--no-session-persistence`. Never `--bare` (it drops subscription auth).
- All state files `0600`, directories `0700`, writes atomic (temp file + rename).
- Default delivery mode `queue`. Inject messages capped at 2000 characters (rune-safe).
- Suggested keys: `prefix+d` manage, `prefix+a` toggle pane, `prefix+shift+s` summon. Never `prefix+s` or `prefix+shift+d` (Herdr built-ins).
- Tests never touch the user's real Herdr session (`~/.config/herdr/herdr.sock`). Live experiments use `herdr --session dots-test`.
- Run `go test -race ./...` and `go vet ./...` before every commit.

## Review Focus

1. **Long Unix socket paths.** macOS limits socket paths to 104 bytes; a deep `HERDR_PLUGIN_STATE_DIR` must fall back to a short temp path instead of failing to listen. (Task 3)
2. **Huge or partial transcript lines.** Claude writes multi-megabyte JSONL lines (tool results) and the last line may be half-written while the engine reads; parsing must not hit a 64 KB scanner limit or abort on one bad line. (Task 6)
3. **Model output that isn't clean JSON.** Models wrap JSON in code fences or prose, use `"warning"` for severity, or say `relevant:true` with an empty message; parsing must be tolerant where safe and reject where not. (Task 8)
4. **Multi-byte text at the inject cap.** Truncating a 2000-character inject message must never cut a UTF-8 rune or emoji glyph in half. (Task 13)
5. **`/clear` or agent restart in the same pane.** The agent's session id changes and the old transcript stops growing; the engine must notice the new session and follow it instead of going silent. (Task 14)

---

## File Structure

```
herdr-plugin.toml                 Herdr manifest
Makefile                          build / test / vet / link
scripts/build.sh                  [[build]] step: go build -o bin/herdr-dots ./cmd/herdr-dots
README.md                         install, keys, privacy, scaffold status, roadmap
cmd/herdr-dots/
  main.go                         subcommand dispatch
  engine.go                       engine subcommand, ensureEngine, herdr liveness watch
  ui.go                           target resolution, pane/manage/summon-ui runners
  action.go                       action subcommand (manage, toggle-pane, summon, pause-all)
  setupkeys.go                    setup-keys subcommand
internal/version/version.go       Version constant
internal/fsutil/atomic.go         WriteFileAtomic
internal/paths/paths.go           plugin dirs, session key, file locations
internal/config/config.go         global config.toml
internal/lockfile/lockfile.go     flock-based single-engine lock
internal/dots/dot.go              Dot model, defaults, Slugify, Validate
internal/dots/store.go            LoadAll, Load, Save, Delete
internal/dots/templates.go        embedded templates
internal/dots/templates/*.toml    7 built-in templates
internal/herdr/types.go           PaneInfo, AgentStatus, InvocationContext
internal/herdr/client.go          Client interface + SocketClient
internal/herdr/herdrtest/fake.go  fake Herdr socket server for tests
internal/transcript/turn.go       Turn, ToolUse, Format, dot prefixes
internal/transcript/claude.go     Claude Code JSONL reader
internal/transcript/codex.go      Codex session reader
internal/transcript/scrollback.go scrollback reader
internal/transcript/select.go     reader selection
internal/backend/backend.go       Backend interface, Request, Result
internal/backend/prompt.go        system/user prompts
internal/backend/verdict.go       ParseVerdict
internal/backend/exec.go          Cmd, Runner, ExecRunner, CleanEnv, Hint
internal/backend/claude.go        claude-cli backend
internal/backend/codex.go         codex-cli backend
internal/backend/registry.go      New(kind)
internal/queue/queue.go           Card store + state machine
internal/state/attachments.go     pane ↔ dots attachments
internal/state/usage.go           calls/tokens per dot and pane
internal/state/feed.go            run outcome log
internal/view/view.go             Snapshot types shared by engine, ipc and UIs
internal/engine/filter.go         trigger filter (Decide)
internal/engine/engine.go         engine core
internal/engine/deliver.go        delivery, liveness, session tracking
internal/engine/handle.go         IPC command handling
internal/delivery/delivery.go     formatting, guards, Send
internal/ipc/ipc.go               protocol, Serve, Client
internal/keys/keys.go             setup-keys logic
internal/ui/style/style.go        shared colors and styles
internal/ui/pane/model.go         Dots side pane
internal/ui/manage/model.go       library screens
internal/ui/manage/form.go        dot editor form
internal/ui/summon/model.go       summon popup
docs/superpowers/notes/2026-10-08-verification.md   Task 1 findings
```

---

### Task 1: Live verification spike (V1–V9)

This task produces **findings, not product code**. It answers the spec's verification questions against a real Herdr server in an isolated session. If a finding contradicts an assumption below, stop and report to the user before Task 2.

Already confirmed from `herdr api schema` and local checks (record these in the notes file as "confirmed by schema"):
- `pane.get` → `{"type":"pane_info","pane":{...}}`; `pane.agent_session` is `{"source","agent","kind":"id"|"path","value"}` or `null`.
- `plugin.pane.open` params: `plugin_id`, `entrypoint`, `placement`, `direction` (`right`|`down`), `target_pane_id`, `width`, `height`, `env`, `focus`, `cwd`.
- Subscription stream kinds: only `pane.agent_status_changed`, `pane.output_matched`, `pane.scroll_changed` (no pane-closed event → engine polls).
- `claude -p --output-format json` returns `{"type":"result","is_error":bool,"result":string,"usage":{"input_tokens","output_tokens"},"total_cost_usd":number,...}`.
- `--bare` forces `ANTHROPIC_API_KEY` auth → not usable. `--settings '{"disableAllHooks":true}'` works with subscription auth.
- Codex CLI is **not installed** on the dev machine.

**Files:**
- Create: `docs/superpowers/notes/2026-10-08-verification.md`
- Create: `internal/transcript/testdata/claude_real.jsonl` (redacted capture)

- [ ] **Step 1: Start an isolated headless Herdr server**

Run (in background): `herdr --session dots-test server`
Then: `herdr --session dots-test status server`
Expected: server reported running. All later commands in this task use `--session dots-test`. Never run `server stop` without `--session dots-test`.

- [ ] **Step 2: Create a workspace with a Claude pane in a scratch repo**

```bash
mkdir -p /tmp/dots-test-repo && cd /tmp/dots-test-repo && git init -q && echo 'export const token = localStorage.getItem("jwt")' > auth.ts
herdr --session dots-test workspace create --help   # confirm flag names
herdr --session dots-test workspace create --cwd /tmp/dots-test-repo
herdr --session dots-test pane list
```
Record the pane id (e.g. `w1:p1`). Then start Claude in it:
`herdr --session dots-test agent start dots-main --kind claude --pane <pane_id>`

- [ ] **Step 3: V1 — session identity**

Send a prompt and wait: `herdr --session dots-test agent prompt <pane_id> "Read auth.ts and summarize it in one sentence" --wait --until idle --timeout 120000`
Then capture raw JSON: `printf '%s\n' '{"id":"1","method":"pane.get","params":{"pane_id":"<pane_id>"}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock`
Record: `agent`, `agent_status`, `agent_session.kind`, `agent_session.value`, `cwd`, `foreground_cwd`. If `kind` is `id`, confirm the file exists: `ls ~/.claude/projects/*/<value>.jsonl`.

- [ ] **Step 4: V2 — status transitions during a tool-using turn**

In a second background shell, subscribe and log for one turn:
```bash
printf '%s\n' '{"id":"s","method":"events.subscribe","params":{"subscriptions":[{"type":"pane.agent_status_changed","pane_id":"<pane_id>"}]}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock | while read -r l; do echo "$(date +%T.%N) $l"; done > /tmp/dots-v2.log
```
Prompt a multi-tool turn: `herdr --session dots-test agent prompt <pane_id> "Grep for localStorage, then read auth.ts, then explain the risk" --wait --until idle --timeout 180000`
Record from `/tmp/dots-v2.log`: the exact envelope JSON of one event, every transition, and the longest `idle` gap between `working` periods inside the turn. **Finding needed:** is 1500 ms debounce enough? If any intra-turn idle gap exceeds 1200 ms, record the recommended `turn_debounce_ms` (gap + 1000 ms).

- [ ] **Step 5: V6 — prompt delivery into the Claude TUI**

Run: `herdr --session dots-test agent prompt <pane_id> $'[◆ Security dot] JWT in localStorage is XSS-stealable.\n\nUse an httpOnly cookie.' --wait --until idle --timeout 120000`
Then: `herdr --session dots-test pane read <pane_id> --source recent --lines 40`
Record: was the multi-line text submitted as ONE prompt? Does the transcript user record contain the `[◆ Security dot]` prefix at the start? Does `agent prompt` accept a pane id as `target`?

- [ ] **Step 6: V3 — open a split beside a specific pane**

Link a throwaway manifest to test `plugin.pane.open`:
```bash
mkdir -p /tmp/dots-probe && cat > /tmp/dots-probe/herdr-plugin.toml <<'EOF'
id = "dots-probe"
name = "Dots probe"
version = "0.0.1"
min_herdr_version = "0.8.0"
[[panes]]
id = "side"
title = "Probe"
placement = "split"
command = ["sh", "-c", "env | grep -E 'HERDR|PROBE' ; sleep 600"]
EOF
herdr --session dots-test plugin link /tmp/dots-probe
printf '%s\n' '{"id":"1","method":"plugin.pane.open","params":{"plugin_id":"dots-probe","entrypoint":"side","placement":"split","direction":"right","target_pane_id":"<pane_id>","env":{"PROBE_TARGET":"<pane_id>"},"focus":false}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock
```
Record: the response JSON (does it contain `plugin_pane.pane.pane_id`?), whether the split opened next to `<pane_id>`, whether `PROBE_TARGET` appears in the pane's env (`herdr --session dots-test pane read <new_pane> --source visible`), the default split ratio, and whether `pane.resize` with `{"pane_id":"<new>","direction":"left","amount":0.15}` narrows it. Then `plugin.pane.close` it and `herdr --session dots-test plugin unlink dots-probe`.

- [ ] **Step 7: V7 — dot CLI runs don't touch Herdr state**

From a shell inside the dots-test pane environment (so `HERDR_*` vars exist), run the dot command with a stripped env:
```bash
cd /tmp/dots-test-repo && env -u HERDR_PANE_ID -u HERDR_SOCKET_PATH -u HERDR_ENV -u CLAUDECODE claude -p --output-format json --model haiku --tools "" --no-session-persistence --strict-mcp-config --settings '{"disableAllHooks":true}' 'Reply: {"relevant":false,"severity":"info","message":""}'
```
Check during the run: `herdr --session dots-test agent list`. Record: no new agent appears and the main pane's status did not change. Also confirm no new file under `~/.claude/projects/-tmp-dots-test-repo/` (session persistence off).

- [ ] **Step 8: V8 + V9 — keys and sidebar metadata**

V8: `grep -n 'prefix+d\|prefix+a\|prefix+shift+s' ~/.config/herdr/config.toml` (expected: no matches) and `herdr --default-config | grep -E '"prefix\+(d|a|shift\+s)"'` (expected: no matches). Record.
V9: report pending metadata and look at the sidebar (`herdr --session dots-test` attach in a real terminal is optional; otherwise read `pane.get` back):
```bash
printf '%s\n' '{"id":"1","method":"pane.report_metadata","params":{"pane_id":"<pane_id>","source":"herdr-dots","display_agent":"claude ◆2"}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock
printf '%s\n' '{"id":"2","method":"pane.get","params":{"pane_id":"<pane_id>"}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock
printf '%s\n' '{"id":"3","method":"pane.report_metadata","params":{"pane_id":"<pane_id>","source":"herdr-dots","clear_display_agent":true}}' | nc -U ~/.config/herdr/sessions/dots-test/herdr.sock
```
Record: does `display_agent` become `claude ◆2`, and does clearing restore it? Does Claude's own status detection keep working afterwards?

- [ ] **Step 9: V5 — capture a redacted Claude transcript fixture**

```bash
F=$(ls ~/.claude/projects/*/$(printf '%s' '<session_id>').jsonl)
mkdir -p internal/transcript/testdata
jq -c 'del(.toolUseResult) | if .message.content|type=="array" then .message.content |= map(if .type=="tool_result" then .content="<redacted>" else . end) else . end' "$F" > internal/transcript/testdata/claude_real.jsonl
```
Open the file and confirm it holds only the scratch-repo conversation (no secrets, no personal paths beyond `/tmp/dots-test-repo`). Record the record types present.

- [ ] **Step 10: V4/V5/V6 for Codex**

Run `which codex`. Codex is expected to be missing: record "BLOCKED — codex not installed; codex reader/backend are built from the documented formats in Tasks 7 and 9 and must be live-verified before any codex dot is relied on." Ask the user whether to install Codex now (`npm i -g @openai/codex`) or defer.

- [ ] **Step 11: Write findings and stop the test server**

Write `docs/superpowers/notes/2026-10-08-verification.md` with one section per V-question: question, command(s), raw evidence excerpt, finding, and "assumption holds: yes/no". Then: `herdr --session dots-test server stop`.

If any "assumption holds: no", STOP and report to the user with the proposed spec change before continuing.

- [ ] **Step 12: Commit**

```bash
git add docs/superpowers/notes/2026-10-08-verification.md internal/transcript/testdata/claude_real.jsonl
git commit -m "docs: record live Herdr/Claude verification findings for dots v1"
```

---

### Task 2: Project scaffold, manifest and build

**Files:**
- Create: `go.mod`, `Makefile`, `scripts/build.sh`, `herdr-plugin.toml`, `.gitignore`
- Create: `cmd/herdr-dots/main.go`, `internal/version/version.go`
- Test: `cmd/herdr-dots/main_test.go`

**Interfaces:**
- Produces: `version.Version = "0.1.0"`; `main.dispatch(args []string) error` routes `version`, `engine`, `pane`, `manage`, `summon-ui`, `action`, `setup-keys`. Later tasks add the handlers named `runEngine`, `runPane`, `runManage`, `runSummonUI`, `runAction`, `runSetupKeys` (all `func(args []string) error`). Until then each is registered as `notYet`.

- [ ] **Step 1: Initialize the module and dependencies**

```bash
go mod init herdr-dots
go mod edit -go=1.25
go get github.com/BurntSushi/toml@v1.6.0 github.com/charmbracelet/bubbletea@v1.3.10 github.com/charmbracelet/bubbles@v1.0.0 github.com/charmbracelet/lipgloss@v1.1.0 github.com/bmatcuk/doublestar/v4@latest
```

Create `.gitignore`:
```
/bin/
*.test
```

- [ ] **Step 2: Write the failing test**

`cmd/herdr-dots/main_test.go`:
```go
package main

import (
	"strings"
	"testing"
)

func TestDispatchUnknownCommand(t *testing.T) {
	err := dispatch([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("want unknown command error, got %v", err)
	}
}

func TestDispatchNoArgsShowsUsage(t *testing.T) {
	err := dispatch(nil)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestDispatchVersion(t *testing.T) {
	if err := dispatch([]string{"version"}); err != nil {
		t.Fatalf("version: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/herdr-dots/`
Expected: FAIL — `undefined: dispatch`.

- [ ] **Step 4: Write the implementation**

`internal/version/version.go`:
```go
// Package version holds the herdr-dots version. v1 is a scaffold: stay on 0.x.
package version

const Version = "0.1.0"
```

`cmd/herdr-dots/main.go`:
```go
// Command herdr-dots is the single binary behind the Herdr Dots plugin. Herdr
// launches its subcommands from herdr-plugin.toml.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"herdr-dots/internal/version"
)

type handler func(args []string) error

func notYet(name string) handler {
	return func([]string) error { return fmt.Errorf("%s: not implemented yet", name) }
}

var commands = map[string]handler{
	"version": func([]string) error {
		fmt.Println("herdr-dots", version.Version, "(scaffold)")
		return nil
	},
	"engine":     notYet("engine"),
	"pane":       notYet("pane"),
	"manage":     notYet("manage"),
	"summon-ui":  notYet("summon-ui"),
	"action":     notYet("action"),
	"setup-keys": notYet("setup-keys"),
}

func usage() error {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return errors.New("usage: herdr-dots <" + strings.Join(names, "|") + "> [args]")
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	h, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q (%v)", args[0], usage())
	}
	return h(args[1:])
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots:", err)
		os.Exit(1)
	}
}
```

When a later task implements a handler, it replaces the `notYet(...)` entry in `commands` with the real function.

- [ ] **Step 5: Add build script, Makefile and manifest**

`scripts/build.sh`:
```sh
#!/bin/sh
# Herdr runs this as the manifest's [[build]] step, in the plugin root.
set -eu
mkdir -p bin
if ! command -v go >/dev/null 2>&1; then
	echo "herdr-dots: Go toolchain (>= 1.25) is required to build" >&2
	exit 1
fi
exec go build -o bin/herdr-dots ./cmd/herdr-dots
```
Run: `chmod +x scripts/build.sh`

`Makefile`:
```make
.PHONY: build test vet link
build:
	sh scripts/build.sh
test:
	go test -race ./...
vet:
	go vet ./...
link: build
	herdr --session dots-test plugin link .
```

`herdr-plugin.toml`:
```toml
# Herdr Dots — always-on specialist agents for Herdr. v1 is a scaffold (0.x).
id = "herdr-dots"
name = "Herdr Dots"
version = "0.1.0"
min_herdr_version = "0.8.0"
description = "Specialist agents (dots) that join your Claude Code / Codex sessions and review each turn. Scaffold release."
platforms = ["macos", "linux"]

[[build]]
command = ["sh", "scripts/build.sh"]

[[startup]]
command = ["./bin/herdr-dots", "engine", "--ensure"]

[[actions]]
id = "manage"
title = "Dots: manage"
command = ["./bin/herdr-dots", "action", "manage"]

[[actions]]
id = "toggle-pane"
title = "Dots: toggle pane"
command = ["./bin/herdr-dots", "action", "toggle-pane"]

[[actions]]
id = "summon"
title = "Dots: summon"
command = ["./bin/herdr-dots", "action", "summon"]

[[actions]]
id = "pause-all"
title = "Dots: pause/resume all"
command = ["./bin/herdr-dots", "action", "pause-all"]

[[panes]]
id = "pane"
title = "Dots"
placement = "split"
command = ["./bin/herdr-dots", "pane"]

[[panes]]
id = "manage"
title = "Dots: manage"
placement = "popup"
width = "80%"
height = "80%"
command = ["./bin/herdr-dots", "manage"]

[[panes]]
id = "summon"
title = "Dots: summon"
placement = "popup"
width = "60%"
height = 14
command = ["./bin/herdr-dots", "summon-ui"]
```

- [ ] **Step 6: Run tests and build**

Run: `go test ./cmd/herdr-dots/ && make build && ./bin/herdr-dots version`
Expected: tests PASS; prints `herdr-dots 0.1.0 (scaffold)`.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum .gitignore Makefile scripts/build.sh herdr-plugin.toml cmd internal/version
git commit -m "chore: scaffold herdr-dots binary, manifest and build"
```

---

### Task 3: Atomic writes, plugin paths and global config

**Files:**
- Create: `internal/fsutil/atomic.go`, `internal/paths/paths.go`, `internal/config/config.go`
- Test: `internal/fsutil/atomic_test.go`, `internal/paths/paths_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `fsutil.WriteFileAtomic(path string, data []byte, perm os.FileMode) error`
  - `paths.Paths{ConfigDir, StateDir, SocketPath, SessionDir string}`; `paths.New(configDir, stateDir, herdrSocket string) Paths`; `paths.FromEnv() (Paths, error)`; `paths.SessionKey(socket string) string`; methods `DotsDir() ConfigFile() EngineLock() EngineLog() EngineSock() AttachmentsFile() QueueFile() FeedFile() UsageFile() DotsPanesFile() string` and `Ensure() error`.
  - `config.Config{DefaultBackend, DefaultModel string; SessionBudgetCalls, MaxParallelRuns, RunTimeoutSeconds, TurnDebounceMs int; InjectEnabled bool}`; `config.Default() Config`; `config.Load(path string) (Config, error)`; `(Config) RunTimeout() time.Duration`; `(Config) Debounce() time.Duration`.

- [ ] **Step 1: Write the failing tests**

`internal/fsutil/atomic_test.go`:
```go
package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesWithPerm(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	if err := WriteFileAtomic(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello" {
		t.Fatalf("content = %q", got)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", st.Mode().Perm())
	}
	dst, _ := os.Stat(filepath.Dir(p))
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %v", dst.Mode().Perm())
	}
}

func TestWriteFileAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = WriteFileAtomic(p, []byte("one"), 0o600)
	_ = WriteFileAtomic(p, []byte("two"), 0o600)
	got, _ := os.ReadFile(p)
	if string(got) != "two" {
		t.Fatalf("content = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}
```

`internal/paths/paths_test.go`:
```go
package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionKeyStableAndShort(t *testing.T) {
	a := SessionKey("/x/herdr.sock")
	if a != SessionKey("/x/herdr.sock") || len(a) != 12 {
		t.Fatalf("key %q", a)
	}
	if a == SessionKey("/y/herdr.sock") {
		t.Fatal("different sockets must give different keys")
	}
}

func TestFilesLiveUnderSessionDir(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.SessionDir != filepath.Join("/s", SessionKey("/sock")) {
		t.Fatalf("session dir %q", p.SessionDir)
	}
	for _, f := range []string{p.EngineLock(), p.QueueFile(), p.AttachmentsFile(), p.FeedFile(), p.UsageFile(), p.EngineLog(), p.DotsPanesFile()} {
		if !strings.HasPrefix(f, p.SessionDir) {
			t.Fatalf("%s not under session dir", f)
		}
	}
	if p.DotsDir() != "/c/dots" || p.ConfigFile() != "/c/config.toml" {
		t.Fatal("config paths wrong")
	}
}

// Review Focus 1: macOS caps unix socket paths at 104 bytes.
func TestEngineSockFallsBackWhenPathTooLong(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	p := New("/c", long, "/sock")
	s := p.EngineSock()
	if len(s) > 100 {
		t.Fatalf("socket path too long (%d): %s", len(s), s)
	}
	if !strings.HasPrefix(s, os.TempDir()) {
		t.Fatalf("expected temp-dir fallback, got %s", s)
	}
	short := New("/c", "/s", "/sock").EngineSock()
	if short != filepath.Join(New("/c", "/s", "/sock").SessionDir, "dots.sock") {
		t.Fatalf("short path should stay in session dir: %s", short)
	}
}

func TestFromEnvRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("want error outside herdr")
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "/c")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/s")
	t.Setenv("HERDR_SOCKET_PATH", "/sock")
	p, err := FromEnv()
	if err != nil || p.ConfigDir != "/c" {
		t.Fatalf("FromEnv: %v %+v", err, p)
	}
}

func TestEnsureCreatesPrivateDirs(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "c"), filepath.Join(root, "s"), "/sock")
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir} {
		st, err := os.Stat(d)
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", d, err, st)
		}
	}
}
```

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || c != Default() {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestLoadPartialFileMergesOverDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 5\ninject_enabled = false\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxParallelRuns != 5 || c.InjectEnabled || c.DefaultBackend != "claude-cli" || c.SessionBudgetCalls != 120 {
		t.Fatalf("merged = %+v", c)
	}
}

func TestLoadClampsNonsense(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 0\nrun_timeout_seconds = 1\nturn_debounce_ms = -5\nsession_budget_calls = 0\n"), 0o600)
	c, _ := Load(p)
	if c.MaxParallelRuns != 1 || c.RunTimeoutSeconds != 10 || c.TurnDebounceMs != 0 || c.SessionBudgetCalls != 1 {
		t.Fatalf("clamped = %+v", c)
	}
}

func TestLoadBadTOMLReturnsDefaultsAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = = 3"), 0o600)
	c, err := Load(p)
	if err == nil || c != Default() {
		t.Fatalf("want defaults + error, got %+v %v", c, err)
	}
}

func TestDurations(t *testing.T) {
	c := Default()
	if c.RunTimeout() != 120*time.Second || c.Debounce() != 1500*time.Millisecond {
		t.Fatal("durations")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/fsutil/ ./internal/paths/ ./internal/config/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write the implementations**

`internal/fsutil/atomic.go`:
```go
// Package fsutil holds small filesystem helpers shared by the state stores.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to a temp file in the same directory and renames
// it over path, so readers never observe a half-written file. The parent
// directory is created with 0700.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once renamed
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

`internal/paths/paths.go`:
```go
// Package paths resolves every file herdr-dots reads or writes. Herdr gives
// each plugin a config dir (user-editable) and a state dir (runtime). State is
// shared across named Herdr sessions, so runtime files live under a per-session
// subdirectory keyed by the Herdr socket path.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// maxSockPath stays under the 104-byte sun_path limit on macOS.
const maxSockPath = 100

type Paths struct {
	ConfigDir  string
	StateDir   string
	SocketPath string // Herdr's socket
	SessionDir string // StateDir/<SessionKey(SocketPath)>
}

func New(configDir, stateDir, herdrSocket string) Paths {
	return Paths{
		ConfigDir:  configDir,
		StateDir:   stateDir,
		SocketPath: herdrSocket,
		SessionDir: filepath.Join(stateDir, SessionKey(herdrSocket)),
	}
}

// FromEnv reads the directories Herdr injects into every plugin command.
func FromEnv() (Paths, error) {
	c := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	s := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	h := os.Getenv("HERDR_SOCKET_PATH")
	if c == "" || s == "" || h == "" {
		return Paths{}, errors.New("HERDR_PLUGIN_CONFIG_DIR, HERDR_PLUGIN_STATE_DIR and HERDR_SOCKET_PATH must be set; are you running inside herdr?")
	}
	return New(c, s, h), nil
}

func SessionKey(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])[:12]
}

func (p Paths) DotsDir() string         { return filepath.Join(p.ConfigDir, "dots") }
func (p Paths) ConfigFile() string      { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) EngineLock() string      { return filepath.Join(p.SessionDir, "engine.lock") }
func (p Paths) EngineLog() string       { return filepath.Join(p.SessionDir, "engine.log") }
func (p Paths) AttachmentsFile() string { return filepath.Join(p.SessionDir, "attachments.json") }
func (p Paths) QueueFile() string       { return filepath.Join(p.SessionDir, "queue.json") }
func (p Paths) FeedFile() string        { return filepath.Join(p.SessionDir, "feed.jsonl") }
func (p Paths) UsageFile() string       { return filepath.Join(p.SessionDir, "usage.json") }
func (p Paths) DotsPanesFile() string   { return filepath.Join(p.SessionDir, "dotspanes.json") }

// EngineSock is the engine's IPC socket. Deep state dirs would exceed the unix
// socket path limit, so those fall back to a short path in the temp dir.
func (p Paths) EngineSock() string {
	s := filepath.Join(p.SessionDir, "dots.sock")
	if len(s) <= maxSockPath {
		return s
	}
	return filepath.Join(os.TempDir(), "herdr-dots-"+filepath.Base(p.SessionDir)+".sock")
}

func (p Paths) Ensure() error {
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
```

`internal/config/config.go`:
```go
// Package config loads the plugin-wide settings from CONFIG_DIR/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DefaultBackend     string `toml:"default_backend"`
	DefaultModel       string `toml:"default_model"`
	SessionBudgetCalls int    `toml:"session_budget_calls"`
	MaxParallelRuns    int    `toml:"max_parallel_runs"`
	RunTimeoutSeconds  int    `toml:"run_timeout_seconds"`
	TurnDebounceMs     int    `toml:"turn_debounce_ms"`
	InjectEnabled      bool   `toml:"inject_enabled"`
}

func Default() Config {
	return Config{
		DefaultBackend:     "claude-cli",
		DefaultModel:       "sonnet",
		SessionBudgetCalls: 120,
		MaxParallelRuns:    3,
		RunTimeoutSeconds:  120,
		TurnDebounceMs:     1500,
		InjectEnabled:      true,
	}
}

// Load returns Default() when the file is missing. Keys present in the file
// override defaults; absent keys keep them. A malformed file yields Default()
// plus an error so the caller can log it and keep running.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if _, err := toml.Decode(string(data), &c); err != nil {
		return Default(), fmt.Errorf("config %s: %w", path, err)
	}
	if c.MaxParallelRuns < 1 {
		c.MaxParallelRuns = 1
	}
	if c.RunTimeoutSeconds < 10 {
		c.RunTimeoutSeconds = 10
	}
	if c.TurnDebounceMs < 0 {
		c.TurnDebounceMs = 0
	}
	if c.SessionBudgetCalls < 1 {
		c.SessionBudgetCalls = 1
	}
	return c, nil
}

func (c Config) RunTimeout() time.Duration { return time.Duration(c.RunTimeoutSeconds) * time.Second }
func (c Config) Debounce() time.Duration   { return time.Duration(c.TurnDebounceMs) * time.Millisecond }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/fsutil/ ./internal/paths/ ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/fsutil internal/paths internal/config
git commit -m "feat: atomic writes, per-session plugin paths and global config"
```

---

### Task 4: Dot definitions, validation, storage and templates

**Files:**
- Create: `internal/dots/dot.go`, `internal/dots/store.go`, `internal/dots/templates.go`, `internal/dots/templates/{security,frontend,backend,copywriter,marketing,spec-critic,test-writer}.toml`
- Test: `internal/dots/dot_test.go`, `internal/dots/store_test.go`, `internal/dots/templates_test.go`

**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`.
- Produces:
  - `type Mode string`; consts `ModeObserve`, `ModeSuggest`, `ModeQueue`, `ModeInject`; `var Modes []Mode`; `(Mode) Next() Mode`; `(Mode) Valid() bool`.
  - `var BackendKinds = []string{"claude-cli", "codex-cli"}`; `var Verbosities = []string{"terse", "normal", "detailed"}`; `var Colors = []string{"red","green","yellow","blue","magenta","cyan","white"}`.
  - `type Dot struct{ Slug string; Name, Glyph, Color, Purpose string; Backend BackendSpec; Delivery DeliverySpec; Trigger TriggerSpec; Context ContextSpec; Budget BudgetSpec }` with `BackendSpec{Kind, Model string; Args []string}`, `DeliverySpec{Mode Mode}`, `TriggerSpec{OnTurn bool; FileGlobs []string; Cooldown int}`, `ContextSpec{Turns int; RepoRead bool; Verbosity string}`, `BudgetSpec{MaxCallsPerSession int}`.
  - `New(name string) Dot`; `Slugify(name string) string`; `(Dot) Validate() error`.
  - `type LoadError struct{ File string; Line int; Err error }` (+ `Error() string`); `LoadAll(dir string) ([]Dot, []LoadError)`; `Load(dir, slug string) (Dot, error)`; `Save(dir string, d Dot, create bool) error`; `var ErrExists`; `Delete(dir, slug string) error`.
  - `Templates() []Dot`; `FromTemplate(slug string) (Dot, error)`.

- [ ] **Step 1: Write the failing tests**

`internal/dots/dot_test.go`:
```go
package dots

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Security":          "security",
		"Front End  Dot!":   "front-end-dot",
		"  --Spec Critic-- ": "spec-critic",
		"ñandú ✨":           "and",
		"!!!":               "dot",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewIsValidOnceNamedAndPurposed(t *testing.T) {
	d := New("Security")
	if d.Slug != "security" || d.Delivery.Mode != ModeQueue || d.Context.Turns != 3 {
		t.Fatalf("defaults: %+v", d)
	}
	d.Purpose = "find bugs"
	if err := d.Validate(); err != nil {
		t.Fatalf("valid dot rejected: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	d := New("")
	d.Purpose = " "
	d.Backend.Kind = "gpt-web"
	d.Delivery.Mode = "shout"
	d.Context.Turns = 0
	d.Context.Verbosity = "loud"
	d.Trigger.Cooldown = -1
	d.Trigger.FileGlobs = []string{"src/[unclosed"}
	d.Budget.MaxCallsPerSession = 0
	err := d.Validate()
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"name", "purpose", "backend", "mode", "turns", "verbosity", "cooldown", "glob", "budget"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestModeNextCycles(t *testing.T) {
	if ModeObserve.Next() != ModeSuggest || ModeInject.Next() != ModeObserve || Mode("junk").Next() != ModeQueue {
		t.Fatal("mode cycle")
	}
}
```

`internal/dots/store_test.go`:
```go
package dots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid(name string) Dot {
	d := New(name)
	d.Purpose = "Line one.\nLine two."
	return d
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := valid("Security")
	d.Trigger.FileGlobs = []string{"**/*.ts"}
	if err := Save(dir, d, true); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "security")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Security" || got.Purpose != d.Purpose || got.Trigger.FileGlobs[0] != "**/*.ts" || got.Slug != "security" {
		t.Fatalf("round trip: %+v", got)
	}
	st, _ := os.Stat(filepath.Join(dir, "security.toml"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "security.toml"))
	if !strings.Contains(string(raw), `"""`) {
		t.Fatalf("purpose should be written as a multi-line string:\n%s", raw)
	}
}

func TestSaveCreateRefusesDuplicateSlug(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Security"), true)
	err := Save(dir, valid("security!"), true)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if err := Save(dir, valid("Security"), false); err != nil {
		t.Fatalf("update must succeed: %v", err)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	if err := Save(t.TempDir(), New("x"), true); err == nil {
		t.Fatal("missing purpose must be rejected")
	}
}

func TestLoadAllSkipsInvalidAndReportsLine(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Good"), true)
	os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("name = \"B\"\npurpose = = \"x\"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "empty.toml"), []byte("name = \"E\"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600)
	list, errs := LoadAll(dir)
	if len(list) != 1 || list[0].Slug != "good" {
		t.Fatalf("list = %+v", list)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %+v", errs)
	}
	var broken LoadError
	for _, e := range errs {
		if e.File == "broken.toml" {
			broken = e
		}
	}
	if broken.Line != 2 {
		t.Fatalf("broken line = %d (%v)", broken.Line, broken.Err)
	}
}

func TestLoadAllMissingDirIsEmpty(t *testing.T) {
	list, errs := LoadAll(filepath.Join(t.TempDir(), "nope"))
	if list != nil || errs != nil {
		t.Fatal("missing dir should be empty, not an error")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	_ = Save(dir, valid("Gone"), true)
	if err := Delete(dir, "gone"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(dir, "gone"); err == nil {
		t.Fatal("deleting twice should error")
	}
}
```

`internal/dots/templates_test.go`:
```go
package dots

import "testing"

func TestTemplatesAreAllValid(t *testing.T) {
	ts := Templates()
	if len(ts) != 7 {
		t.Fatalf("want 7 templates, got %d", len(ts))
	}
	for _, d := range ts {
		if err := d.Validate(); err != nil {
			t.Errorf("template %s invalid: %v", d.Slug, err)
		}
	}
}

func TestFromTemplate(t *testing.T) {
	d, err := FromTemplate("security")
	if err != nil || d.Name != "Security" || d.Glyph != "◆" {
		t.Fatalf("security template: %+v %v", d, err)
	}
	if _, err := FromTemplate("nope"); err == nil {
		t.Fatal("unknown template must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dots/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/dots/dot.go`**

```go
// Package dots defines a dot (a specialist agent) and stores dot definitions as
// one TOML file per dot in CONFIG_DIR/dots/<slug>.toml.
package dots

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

type Mode string

const (
	ModeObserve Mode = "observe"
	ModeSuggest Mode = "suggest"
	ModeQueue   Mode = "queue"
	ModeInject  Mode = "inject"
)

var Modes = []Mode{ModeObserve, ModeSuggest, ModeQueue, ModeInject}

func (m Mode) Valid() bool { return slices.Contains(Modes, m) }

// Next cycles observe → suggest → queue → inject → observe. Unknown → queue.
func (m Mode) Next() Mode {
	for i, x := range Modes {
		if x == m {
			return Modes[(i+1)%len(Modes)]
		}
	}
	return ModeQueue
}

var (
	BackendKinds = []string{"claude-cli", "codex-cli"}
	Verbosities  = []string{"terse", "normal", "detailed"}
	Colors       = []string{"red", "green", "yellow", "blue", "magenta", "cyan", "white"}
)

type Dot struct {
	Slug     string       `toml:"-"`
	Name     string       `toml:"name"`
	Glyph    string       `toml:"glyph"`
	Color    string       `toml:"color"`
	Purpose  string       `toml:"purpose,multiline"`
	Backend  BackendSpec  `toml:"backend"`
	Delivery DeliverySpec `toml:"delivery"`
	Trigger  TriggerSpec  `toml:"trigger"`
	Context  ContextSpec  `toml:"context"`
	Budget   BudgetSpec   `toml:"budget"`
}

type BackendSpec struct {
	Kind  string   `toml:"kind"`
	Model string   `toml:"model"`
	Args  []string `toml:"args"`
}

type DeliverySpec struct {
	Mode Mode `toml:"mode"`
}

type TriggerSpec struct {
	OnTurn    bool     `toml:"on_turn"`
	FileGlobs []string `toml:"file_globs"`
	Cooldown  int      `toml:"cooldown"`
}

type ContextSpec struct {
	Turns     int    `toml:"turns"`
	RepoRead  bool   `toml:"repo_read"`
	Verbosity string `toml:"verbosity"`
}

type BudgetSpec struct {
	MaxCallsPerSession int `toml:"max_calls_per_session"`
}

// New returns a dot with every default from the spec filled in.
func New(name string) Dot {
	return Dot{
		Slug:     Slugify(name),
		Name:     name,
		Glyph:    "●",
		Color:    "blue",
		Backend:  BackendSpec{Kind: "claude-cli", Model: "sonnet", Args: []string{}},
		Delivery: DeliverySpec{Mode: ModeQueue},
		Trigger:  TriggerSpec{OnTurn: true, FileGlobs: []string{}},
		Context:  ContextSpec{Turns: 3, RepoRead: true, Verbosity: "normal"},
		Budget:   BudgetSpec{MaxCallsPerSession: 40},
	}
}

// Slugify keeps ASCII letters and digits, turns everything else into single
// dashes, and falls back to "dot" when nothing is left.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "dot"
	}
	return s
}

// Validate returns every problem at once, joined, so the editor can show them all.
func (d Dot) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if strings.TrimSpace(d.Name) == "" {
		add("name is required")
	}
	if strings.TrimSpace(d.Purpose) == "" {
		add("purpose is required")
	}
	if n := utf8.RuneCountInString(d.Glyph); n < 1 || n > 2 {
		add("glyph must be 1-2 characters")
	}
	if !slices.Contains(BackendKinds, d.Backend.Kind) {
		add("backend kind %q must be one of %v", d.Backend.Kind, BackendKinds)
	}
	if !d.Delivery.Mode.Valid() {
		add("delivery mode %q must be one of %v", d.Delivery.Mode, Modes)
	}
	if d.Context.Turns < 1 || d.Context.Turns > 20 {
		add("context turns must be 1-20, got %d", d.Context.Turns)
	}
	if !slices.Contains(Verbosities, d.Context.Verbosity) {
		add("verbosity %q must be one of %v", d.Context.Verbosity, Verbosities)
	}
	if d.Trigger.Cooldown < 0 {
		add("cooldown must be >= 0")
	}
	for _, g := range d.Trigger.FileGlobs {
		if !doublestar.ValidatePattern(g) {
			add("file glob %q is invalid", g)
		}
	}
	if d.Budget.MaxCallsPerSession < 1 {
		add("budget max_calls_per_session must be >= 1")
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: Write `internal/dots/store.go`**

```go
package dots

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
)

var ErrExists = errors.New("a dot with this name already exists")

type LoadError struct {
	File string // base name, e.g. "security.toml"
	Line int    // 0 when not a parse error
	Err  error
}

func (e LoadError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %v", e.File, e.Line, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.File, e.Err)
}

func path(dir, slug string) string { return filepath.Join(dir, slug+".toml") }

func decode(file string) (Dot, error) {
	d := New("")
	if _, err := toml.DecodeFile(file, &d); err != nil {
		return Dot{}, err
	}
	d.Slug = strings.TrimSuffix(filepath.Base(file), ".toml")
	return d, nil
}

// LoadAll returns valid dots sorted by name, plus one LoadError per bad file.
// A missing directory is not an error.
func LoadAll(dir string) ([]Dot, []LoadError) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []LoadError{{File: dir, Err: err}}
	}
	var out []Dot
	var bad []LoadError
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		d, err := decode(filepath.Join(dir, e.Name()))
		if err != nil {
			le := LoadError{File: e.Name(), Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			bad = append(bad, le)
			continue
		}
		if err := d.Validate(); err != nil {
			bad = append(bad, LoadError{File: e.Name(), Err: err})
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, bad
}

func Load(dir, slug string) (Dot, error) {
	d, err := decode(path(dir, slug))
	if err != nil {
		return Dot{}, err
	}
	return d, d.Validate()
}

// Save validates and writes d. With create=true it refuses to overwrite an
// existing file (slug collision); with create=false it replaces it.
func Save(dir string, d Dot, create bool) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if create || d.Slug == "" {
		d.Slug = Slugify(d.Name)
	}
	p := path(dir, d.Slug)
	if create {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%w (%s)", ErrExists, d.Slug)
		}
	}
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(d); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, buf.Bytes(), 0o600)
}

func Delete(dir, slug string) error { return os.Remove(path(dir, slug)) }
```

- [ ] **Step 5: Write `internal/dots/templates.go` and the 7 templates**

`internal/dots/templates.go`:
```go
package dots

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed templates/*.toml
var templateFS embed.FS

// Templates returns the built-in dots, sorted by name. Slug = file name.
func Templates() []Dot {
	entries, _ := fs.ReadDir(templateFS, "templates")
	var out []Dot
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			continue
		}
		d := New("")
		if _, err := toml.Decode(string(data), &d); err != nil {
			continue
		}
		d.Slug = strings.TrimSuffix(e.Name(), ".toml")
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func FromTemplate(slug string) (Dot, error) {
	for _, d := range Templates() {
		if d.Slug == slug {
			return d, nil
		}
	}
	return Dot{}, fmt.Errorf("unknown template %q", slug)
}
```

Each template uses this exact layout. Only the header fields and `purpose` differ, as listed below.

`internal/dots/templates/security.toml`:
```toml
name = "Security"
glyph = "◆"
color = "red"
purpose = """
You are a senior application security engineer reviewing work as it happens.
Flag authentication and session flaws, injection (SQL, command, template, XSS),
secrets or credentials in code, config or logs, unsafe deserialization, missing
input validation, insecure defaults, overly broad permissions and risky
dependencies. Name the exact file, line or plan step, explain the attack in one
sentence, and give the concrete fix. Ignore style and non-security concerns."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

The other six files repeat the same `[backend]`, `[delivery]`, `[trigger]`, `[context]` and `[budget]` tables exactly as above, with these header values:

`internal/dots/templates/frontend.toml`: `name = "Frontend"`, `glyph = "▲"`, `color = "cyan"`, purpose:
```
You are a senior frontend engineer. Review UI work for component structure,
state management, accessibility (semantics, focus, contrast, keyboard use),
loading/empty/error states, responsive layout, performance (re-renders, bundle
size, images) and consistency with the existing design system. Point to the
exact component or plan step and propose the specific change.
```

`internal/dots/templates/backend.toml`: `name = "Backend"`, `glyph = "▼"`, `color = "green"`, purpose:
```
You are a senior backend engineer. Review APIs, data models and services for
correctness, error handling, transactions and consistency, N+1 queries and
indexing, idempotency, pagination, timeouts and retries, and migration safety.
Point to the exact endpoint, query or plan step and propose the specific change.
```

`internal/dots/templates/copywriter.toml`: `name = "Copywriter"`, `glyph = "✎"`, `color = "magenta"`, and in `[backend]` use `model = "haiku"`, purpose:
```
You are a senior conversion copywriter. Only speak when the conversation
involves user-facing text: landing pages, onboarding, emails, error messages,
UI labels. Tighten wording, sharpen the value proposition, fix tone and
clarity, and offer one or two rewritten lines the developer can paste.
```

`internal/dots/templates/marketing.toml`: `name = "Marketing"`, `glyph = "★"`, `color = "yellow"`, and in `[backend]` use `model = "haiku"`, purpose:
```
You are a product marketing strategist. Only speak when the work touches
positioning, landing pages, pricing, launch plans or messaging. Check audience
fit, differentiation, proof points, calls to action and SEO basics, and
suggest the single highest-impact improvement.
```

`internal/dots/templates/spec-critic.toml`: `name = "Spec Critic"`, `glyph = "?"`, `color = "blue"`, purpose:
```
You are a skeptical staff engineer reviewing specs and plans. Find ambiguous
requirements, missing edge cases, hidden assumptions, scope creep, untestable
acceptance criteria and steps in the wrong order. Ask the one question that
would most change the plan, or name the gap with a concrete fix.
```

`internal/dots/templates/test-writer.toml`: `name = "Test Writer"`, `glyph = "✓"`, `color = "white"`, purpose:
```
You are a senior test engineer. When code or plans change, name the specific
tests that are missing: edge cases, failure paths, regressions and integration
seams. Give test names and the assertion each should make. Never ask for tests
that already exist in the conversation.
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/dots/`
Expected: PASS. If `TestSaveLoadRoundTrip` fails on the `"""` check, confirm the BurntSushi version is ≥ v1.3 (the `multiline` tag option).

- [ ] **Step 7: Commit**

```bash
git add internal/dots go.mod go.sum
git commit -m "feat: dot model, validation, TOML storage and 7 built-in templates"
```

---

### Task 5: Herdr socket client and fake Herdr server

**Files:**
- Create: `internal/herdr/types.go`, `internal/herdr/client.go`, `internal/herdr/herdrtest/fake.go`
- Test: `internal/herdr/client_test.go`

**Interfaces:**
- Produces:
  - `type AgentStatus string`; consts `StatusIdle`, `StatusWorking`, `StatusBlocked`, `StatusDone`, `StatusUnknown`.
  - `type AgentSession struct{ Source, Agent, Kind, Value string }`.
  - `type PaneInfo struct{ PaneID, TabID, WorkspaceID string; Focused bool; AgentStatus AgentStatus; Agent, Cwd, ForegroundCwd string; AgentSession *AgentSession }`; `(PaneInfo) WorkDir() string`.
  - `type StatusEvent struct{ PaneID string; Status AgentStatus; Agent string }`.
  - `type OpenPane struct{ Entrypoint, Placement, Direction, TargetPaneID string; Width, Height any; Env map[string]string; Focus bool }`.
  - `type Client interface { Ping(ctx) error; PaneGet(ctx, paneID string) (PaneInfo, error); PaneList(ctx) ([]PaneInfo, error); PaneRead(ctx, paneID, source string, lines int) (string, error); AgentPrompt(ctx, target, text string) error; Notify(ctx, title, body string) error; ReportPending(ctx, paneID, agent string, pending int) error; OpenPluginPane(ctx, req OpenPane) (string, error); ClosePluginPane(ctx, paneID string) error; Subscribe(ctx, paneID string) (<-chan StatusEvent, error) }` (all `ctx` are `context.Context`).
  - `type SocketClient struct{ Path, PluginID string; Timeout time.Duration }`; `NewFromEnv() (*SocketClient, error)`.
  - `type Error struct{ Code, Message string }` (+ `Error()`); `IsNotFound(err error) bool`.
  - `type InvocationContext struct{ FocusedPaneID, FocusedPaneAgent, FocusedPaneCwd, TabID, WorkspaceID string }`; `ParseInvocationContext(raw string) (InvocationContext, error)`.
  - `herdrtest.New(t testing.TB) *Server`; `Server.Path`; methods `SetPane(PaneInfo)`, `RemovePane(id string)`, `SetRead(paneID, text string)`, `Push(paneID string, st herdr.AgentStatus)`, `Calls(method string) []Call`, `WaitSubscribed(paneID string, timeout time.Duration) bool`, `SetPromptError(code string)`; `type Call struct{ Method string; Params map[string]any }`.

- [ ] **Step 1: Write the fake server (test infrastructure)**

`internal/herdr/herdrtest/fake.go`:
```go
// Package herdrtest is an in-process fake of Herdr's socket API for tests. It
// implements only the methods herdr-dots uses.
package herdrtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
)

type Call struct {
	Method string
	Params map[string]any
}

type Server struct {
	Path string

	mu        sync.Mutex
	panes     map[string]herdr.PaneInfo
	reads     map[string]string
	calls     []Call
	subs      map[string][]*json.Encoder
	promptErr string
	ln        net.Listener
}

func New(t testing.TB) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "hdt") // short path: unix socket limit
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Path:  filepath.Join(dir, "h.sock"),
		panes: map[string]herdr.PaneInfo{},
		reads: map[string]string{},
		subs:  map[string][]*json.Encoder{},
	}
	s.ln, err = net.Listen("unix", s.Path)
	if err != nil {
		t.Fatal(err)
	}
	go s.accept()
	t.Cleanup(func() { s.ln.Close(); os.RemoveAll(dir) })
	return s
}

func (s *Server) SetPane(p herdr.PaneInfo) { s.mu.Lock(); s.panes[p.PaneID] = p; s.mu.Unlock() }
func (s *Server) RemovePane(id string)     { s.mu.Lock(); delete(s.panes, id); s.mu.Unlock() }
func (s *Server) SetRead(id, text string)  { s.mu.Lock(); s.reads[id] = text; s.mu.Unlock() }
func (s *Server) SetPromptError(code string) {
	s.mu.Lock()
	s.promptErr = code
	s.mu.Unlock()
}

// Push updates the pane's status and emits a pane.agent_status_changed event
// to every subscriber of that pane.
func (s *Server) Push(id string, st herdr.AgentStatus) {
	s.mu.Lock()
	p := s.panes[id]
	p.AgentStatus = st
	s.panes[id] = p
	encs := append([]*json.Encoder(nil), s.subs[id]...)
	s.mu.Unlock()
	for _, enc := range encs {
		_ = enc.Encode(map[string]any{
			"event": "pane.agent_status_changed",
			"data":  map[string]any{"type": "pane_agent_status_changed", "pane_id": id, "workspace_id": "w1", "agent_status": st, "agent": p.Agent},
		})
	}
}

func (s *Server) Calls(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) WaitSubscribed(id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.subs[id])
		s.mu.Unlock()
		if n > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

type req struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func (s *Server) serve(c net.Conn) {
	dec := json.NewDecoder(bufio.NewReader(c))
	enc := json.NewEncoder(c)
	var r req
	if err := dec.Decode(&r); err != nil {
		c.Close()
		return
	}
	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: r.Method, Params: r.Params})
	s.mu.Unlock()

	ok := func(result any) { _ = enc.Encode(map[string]any{"id": r.ID, "result": result}) }
	fail := func(code, msg string) {
		_ = enc.Encode(map[string]any{"id": r.ID, "error": map[string]string{"code": code, "message": msg}})
	}
	str := func(k string) string { v, _ := r.Params[k].(string); return v }

	if r.Method == "events.subscribe" {
		subs, _ := r.Params["subscriptions"].([]any)
		ok(map[string]any{"type": "subscription_started"})
		s.mu.Lock()
		for _, raw := range subs {
			m, _ := raw.(map[string]any)
			id, _ := m["pane_id"].(string)
			s.subs[id] = append(s.subs[id], enc)
		}
		s.mu.Unlock()
		_, _ = dec.Token() // block until the client closes the connection
		s.mu.Lock()
		for id, list := range s.subs {
			kept := list[:0]
			for _, e := range list {
				if e != enc {
					kept = append(kept, e)
				}
			}
			s.subs[id] = kept
		}
		s.mu.Unlock()
		c.Close()
		return
	}
	defer c.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case "ping":
		ok(map[string]any{"type": "pong", "version": "fake"})
	case "pane.get":
		p, found := s.panes[str("pane_id")]
		if !found {
			fail("not_found", "pane not found")
			return
		}
		ok(map[string]any{"type": "pane_info", "pane": p})
	case "pane.list":
		list := []herdr.PaneInfo{}
		for _, p := range s.panes {
			list = append(list, p)
		}
		ok(map[string]any{"type": "pane_list", "panes": list})
	case "pane.read":
		ok(map[string]any{"type": "pane_read", "read": map[string]any{"text": s.reads[str("pane_id")]}})
	case "agent.prompt":
		if s.promptErr != "" {
			fail(s.promptErr, "prompt failed")
			return
		}
		ok(map[string]any{"type": "ok"})
	case "notification.show":
		ok(map[string]any{"type": "notification_show", "shown": true})
	case "pane.report_metadata", "plugin.pane.close":
		ok(map[string]any{"type": "ok"})
	case "plugin.pane.open":
		ok(map[string]any{"type": "plugin_pane_opened", "plugin_pane": map[string]any{"pane": map[string]any{"pane_id": "w1:dots"}}})
	default:
		fail("unknown_method", r.Method)
	}
}
```

Note: the fake serves pane JSON by marshalling `herdr.PaneInfo`, so `PaneInfo`'s JSON tags must match Herdr's field names exactly (Step 4).

- [ ] **Step 2: Write the failing tests**

`internal/herdr/client_test.go`:
```go
package herdr_test

import (
	"context"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
)

func client(s *herdrtest.Server) *herdr.SocketClient {
	return &herdr.SocketClient{Path: s.Path, PluginID: "herdr-dots"}
}

func TestPaneGetAndNotFound(t *testing.T) {
	s := herdrtest.New(t)
	s.SetPane(herdr.PaneInfo{PaneID: "w1:p1", Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Source: "herdr:claude", Agent: "claude", Kind: "id", Value: "abc"}})
	c := client(s)
	p, err := c.PaneGet(context.Background(), "w1:p1")
	if err != nil || p.Agent != "claude" || p.AgentSession.Value != "abc" || p.WorkDir() != "/repo" {
		t.Fatalf("PaneGet = %+v, %v", p, err)
	}
	_, err = c.PaneGet(context.Background(), "nope")
	if !herdr.IsNotFound(err) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestWorkDirPrefersForeground(t *testing.T) {
	p := herdr.PaneInfo{Cwd: "/a", ForegroundCwd: "/b"}
	if p.WorkDir() != "/b" {
		t.Fatal("foreground cwd should win")
	}
}

func TestPromptNotifyReportOpenClose(t *testing.T) {
	s := herdrtest.New(t)
	c := client(s)
	ctx := context.Background()
	if err := c.AgentPrompt(ctx, "w1:p1", "[◆ Security dot] hi"); err != nil {
		t.Fatal(err)
	}
	if got := s.Calls("agent.prompt")[0].Params["text"]; got != "[◆ Security dot] hi" {
		t.Fatalf("prompt text %v", got)
	}
	if err := c.Notify(ctx, "Security dot", "budget reached"); err != nil {
		t.Fatal(err)
	}
	if err := c.ReportPending(ctx, "w1:p1", "claude", 2); err != nil {
		t.Fatal(err)
	}
	if got := s.Calls("pane.report_metadata")[0].Params["display_agent"]; got != "claude ◆2" {
		t.Fatalf("display_agent %v", got)
	}
	_ = c.ReportPending(ctx, "w1:p1", "claude", 0)
	if got := s.Calls("pane.report_metadata")[1].Params["clear_display_agent"]; got != true {
		t.Fatalf("zero pending must clear, got %v", got)
	}
	id, err := c.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "pane", Placement: "split", Direction: "right", TargetPaneID: "w1:p1", Env: map[string]string{"HERDR_DOTS_TARGET": "w1:p1"}})
	if err != nil || id != "w1:dots" {
		t.Fatalf("open = %q %v", id, err)
	}
	open := s.Calls("plugin.pane.open")[0].Params
	if open["plugin_id"] != "herdr-dots" || open["target_pane_id"] != "w1:p1" {
		t.Fatalf("open params %v", open)
	}
	if err := c.ClosePluginPane(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestPaneRead(t *testing.T) {
	s := herdrtest.New(t)
	s.SetRead("w1:p1", "hello\nworld")
	got, err := client(s).PaneRead(context.Background(), "w1:p1", "recent_unwrapped", 200)
	if err != nil || got != "hello\nworld" {
		t.Fatalf("read %q %v", got, err)
	}
	p := s.Calls("pane.read")[0].Params
	if p["source"] != "recent_unwrapped" || p["strip_ansi"] != true {
		t.Fatalf("read params %v", p)
	}
}

func TestSubscribeStreamsStatusAndStopsOnCancel(t *testing.T) {
	s := herdrtest.New(t)
	s.SetPane(herdr.PaneInfo{PaneID: "w1:p1", Agent: "claude"})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := client(s).Subscribe(ctx, "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	if !s.WaitSubscribed("w1:p1", time.Second) {
		t.Fatal("not subscribed")
	}
	s.Push("w1:p1", herdr.StatusWorking)
	s.Push("w1:p1", herdr.StatusIdle)
	for _, want := range []herdr.AgentStatus{herdr.StatusWorking, herdr.StatusIdle} {
		select {
		case ev := <-ch:
			if ev.Status != want || ev.PaneID != "w1:p1" {
				t.Fatalf("event %+v, want %s", ev, want)
			}
		case <-time.After(time.Second):
			t.Fatal("no event")
		}
	}
	cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel should close after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
}

func TestParseInvocationContext(t *testing.T) {
	ic, err := herdr.ParseInvocationContext(`{"focused_pane_id":"w1:p2","focused_pane_agent":"codex","focused_pane_cwd":"/r","tab_id":"w1:t1","workspace_id":"w1"}`)
	if err != nil || ic.FocusedPaneID != "w1:p2" || ic.FocusedPaneAgent != "codex" {
		t.Fatalf("%+v %v", ic, err)
	}
	if ic, err := herdr.ParseInvocationContext(""); err != nil || ic.FocusedPaneID != "" {
		t.Fatal("empty context should be zero value, no error")
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	if _, err := herdr.NewFromEnv(); err == nil {
		t.Fatal("want error without socket")
	}
	t.Setenv("HERDR_SOCKET_PATH", "/x.sock")
	t.Setenv("HERDR_PLUGIN_ID", "herdr-dots")
	c, err := herdr.NewFromEnv()
	if err != nil || c.Path != "/x.sock" || c.PluginID != "herdr-dots" {
		t.Fatalf("%+v %v", c, err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/herdr/...`
Expected: FAIL — undefined `herdr.*`.

- [ ] **Step 4: Write `internal/herdr/types.go`**

```go
// Package herdr is a typed client for the subset of Herdr's socket API that
// herdr-dots uses. Field names match `herdr api schema`.
package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
)

type AgentStatus string

const (
	StatusIdle    AgentStatus = "idle"
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusDone    AgentStatus = "done"
	StatusUnknown AgentStatus = "unknown"
)

// AgentSession is reported by Herdr's agent integrations. Kind is "id" (a
// session id) or "path" (a transcript path).
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type PaneInfo struct {
	PaneID        string        `json:"pane_id"`
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	Focused       bool          `json:"focused"`
	AgentStatus   AgentStatus   `json:"agent_status"`
	Agent         string        `json:"agent"`
	Cwd           string        `json:"cwd"`
	ForegroundCwd string        `json:"foreground_cwd"`
	AgentSession  *AgentSession `json:"agent_session"`
}

// WorkDir is the directory the agent is working in.
func (p PaneInfo) WorkDir() string {
	if p.ForegroundCwd != "" {
		return p.ForegroundCwd
	}
	return p.Cwd
}

type StatusEvent struct {
	PaneID string
	Status AgentStatus
	Agent  string
}

type OpenPane struct {
	Entrypoint   string
	Placement    string // overlay | popup | split | tab | zoomed
	Direction    string // right | down (split only)
	TargetPaneID string
	Width        any // int cells or "80%" (popup only)
	Height       any
	Env          map[string]string
	Focus        bool
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("herdr %s: %s", e.Code, e.Message) }

func IsNotFound(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.Code == "not_found"
}

// InvocationContext is the subset of HERDR_PLUGIN_CONTEXT_JSON herdr-dots reads.
type InvocationContext struct {
	FocusedPaneID    string `json:"focused_pane_id"`
	FocusedPaneAgent string `json:"focused_pane_agent"`
	FocusedPaneCwd   string `json:"focused_pane_cwd"`
	TabID            string `json:"tab_id"`
	WorkspaceID      string `json:"workspace_id"`
}

func ParseInvocationContext(raw string) (InvocationContext, error) {
	var ic InvocationContext
	if raw == "" {
		return ic, nil
	}
	err := json.Unmarshal([]byte(raw), &ic)
	return ic, err
}
```

- [ ] **Step 5: Write `internal/herdr/client.go`**

```go
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
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Path)
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
	sub := map[string]any{"subscriptions": []map[string]any{{"type": "pane.agent_status_changed", "pane_id": paneID}}}
	if err := json.NewEncoder(conn).Encode(request{ID: "herdr-dots-sub", Method: "events.subscribe", Params: sub}); err != nil {
		conn.Close()
		return nil, err
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	var first response
	if err := dec.Decode(&first); err != nil {
		conn.Close()
		return nil, fmt.Errorf("events.subscribe: %w", err)
	}
	if first.Error != nil {
		conn.Close()
		return nil, first.Error
	}
	ch := make(chan StatusEvent, 16)
	go func() { <-ctx.Done(); conn.Close() }()
	go func() {
		defer close(ch)
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
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/herdr/...`
Expected: PASS. If the Task 1 notes show a different envelope shape for subscription events, update the `env` struct in `Subscribe` and the fake's `Push` together to match the notes, then re-run.

- [ ] **Step 7: Commit**

```bash
git add internal/herdr
git commit -m "feat: typed Herdr socket client with subscriptions, plus fake server for tests"
```

---

### Task 6: Turn model and Claude Code transcript reader

**Files:**
- Create: `internal/transcript/turn.go`, `internal/transcript/claude.go`
- Test: `internal/transcript/turn_test.go`, `internal/transcript/claude_test.go`

**Interfaces:**
- Produces:
  - `type ToolUse struct{ Name string; Paths []string }`
  - `type Turn struct{ Index int; User, Assistant string; Tools []ToolUse; FromDots []string }` — `FromDots` lists every dot whose `[<glyph> <Name> dot]` header starts a line of the user message (a delivered dot message). The spec's single `FromDot` becomes a slice because one delivery can batch several dots.
  - `(Turn) TouchedPaths() []string`; `(Turn) IsFromDot(name string) bool`; `(Turn) FromAnyDot() bool`
  - `DotPrefix(glyph, name string) string` → `"[◆ Security dot]"`; `FromDots(text string) []string`
  - `Format(turns []Turn) string`; `const MaxFieldChars = 8000`
  - `type Reader interface{ Turns(ctx context.Context) ([]Turn, error); Source() string }`; consts `SourceClaude = "claude"`, `SourceCodex = "codex"`, `SourceScrollback = "scrollback"`
  - `ParseClaude(r io.Reader) ([]Turn, error)`; `type ClaudeReader struct{ Path string }`; `ResolveClaudePath(home, kind, value string) (string, error)`

- [ ] **Step 1: Write the failing tests**

`internal/transcript/turn_test.go`:
```go
package transcript

import (
	"strings"
	"testing"
)

func TestDotPrefixRoundTrip(t *testing.T) {
	msg := DotPrefix("◆", "Security") + " JWT issue\n\n" + DotPrefix("★", "Spec Critic") + " unclear scope"
	got := FromDots(msg)
	if len(got) != 2 || got[0] != "Security" || got[1] != "Spec Critic" {
		t.Fatalf("FromDots = %v", got)
	}
	if FromDots("I wrote [◆ Security dot] mid-sentence") != nil {
		t.Fatal("prefix must start a line")
	}
}

func TestTurnHelpers(t *testing.T) {
	tr := Turn{Tools: []ToolUse{{Name: "Read", Paths: []string{"a.ts"}}, {Name: "Edit", Paths: []string{"b.tsx", "a.ts"}}}, FromDots: []string{"Security"}}
	if p := tr.TouchedPaths(); len(p) != 2 || p[0] != "a.ts" || p[1] != "b.tsx" {
		t.Fatalf("TouchedPaths = %v", p)
	}
	if !tr.IsFromDot("Security") || tr.IsFromDot("Frontend") || !tr.FromAnyDot() {
		t.Fatal("FromDot helpers")
	}
}

func TestFormatClipsLongFields(t *testing.T) {
	long := strings.Repeat("x", MaxFieldChars+500) + "THE END"
	out := Format([]Turn{{Index: 4, User: "do it", Assistant: long, Tools: []ToolUse{{Name: "Edit", Paths: []string{"a.go"}}}}})
	for _, want := range []string{"### Turn 4", "USER:\ndo it", "THE END", "FILES TOUCHED: a.go", "…"} {
		if !strings.Contains(out, want) {
			t.Errorf("Format missing %q", want)
		}
	}
	if len([]rune(out)) > MaxFieldChars+200 {
		t.Fatalf("not clipped: %d runes", len([]rune(out)))
	}
}
```

`internal/transcript/claude_test.go`:
```go
package transcript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const claudeSample = `{"type":"permission-mode","permissionMode":"default","sessionId":"s1"}
{"type":"user","isSidechain":false,"message":{"role":"user","content":"Add login"},"sessionId":"s1"}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/r/auth.ts"}}]}}
{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent noise"}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/r/login.tsx","old_string":"a","new_string":"b"}}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"Added login form."}]}}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<system-reminder>meta</system-reminder>"}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[◆ Security dot] JWT in localStorage.\n\n[▲ Frontend dot] add a skeleton"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Switched to a cookie."}]}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}
{"type":"user","message":{"role":"user","content":"half-writ`

func TestParseClaudeBuildsTurns(t *testing.T) {
	turns, err := ParseClaude(strings.NewReader(claudeSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d: %+v", len(turns), turns)
	}
	t1 := turns[0]
	if t1.Index != 1 || t1.User != "Add login" || t1.Assistant != "Added login form." {
		t.Fatalf("turn 1 = %+v", t1)
	}
	if p := t1.TouchedPaths(); len(p) != 2 || p[0] != "/r/auth.ts" || p[1] != "/r/login.tsx" {
		t.Fatalf("turn 1 paths = %v", p)
	}
	if strings.Contains(t1.Assistant, "subagent") {
		t.Fatal("sidechain records must be skipped")
	}
	t2 := turns[1]
	if t2.Index != 2 || len(t2.FromDots) != 2 || t2.Assistant != "Switched to a cookie." {
		t.Fatalf("turn 2 = %+v", t2)
	}
}

// Review Focus 2: multi-megabyte lines must not break parsing.
func TestParseClaudeHandlesHugeLines(t *testing.T) {
	huge := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"` + strings.Repeat("z", 3<<20) + `"}]}}`
	src := `{"type":"user","message":{"role":"user","content":"first"}}` + "\n" + huge + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"
	turns, err := ParseClaude(strings.NewReader(src))
	if err != nil || len(turns) != 1 || turns[0].Assistant != "done" {
		t.Fatalf("huge line: %+v %v", turns, err)
	}
}

func TestResolveClaudePath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-r")
	os.MkdirAll(dir, 0o700)
	f := filepath.Join(dir, "abc-123.jsonl")
	os.WriteFile(f, []byte(claudeSample), 0o600)

	if p, err := ResolveClaudePath(home, "id", "abc-123"); err != nil || p != f {
		t.Fatalf("by id: %q %v", p, err)
	}
	if p, err := ResolveClaudePath(home, "path", f); err != nil || p != f {
		t.Fatalf("by path: %q %v", p, err)
	}
	if _, err := ResolveClaudePath(home, "id", "missing"); err == nil {
		t.Fatal("missing id must error")
	}
	if _, err := ResolveClaudePath(home, "id", "../*"); err == nil {
		t.Fatal("glob characters in id must be rejected")
	}
}

func TestClaudeReader(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(f, []byte(claudeSample), 0o600)
	r := ClaudeReader{Path: f}
	turns, err := r.Turns(context.Background())
	if err != nil || len(turns) != 2 || r.Source() != SourceClaude {
		t.Fatalf("%+v %v", turns, err)
	}
}

// Real capture from Task 1. Skipped until the fixture exists.
func TestParseClaudeRealFixture(t *testing.T) {
	f, err := os.Open("testdata/claude_real.jsonl")
	if err != nil {
		t.Skip("no real fixture yet")
	}
	defer f.Close()
	turns, err := ParseClaude(f)
	if err != nil || len(turns) == 0 {
		t.Fatalf("real fixture: %d turns, %v", len(turns), err)
	}
	for _, tr := range turns {
		if strings.TrimSpace(tr.User) == "" {
			t.Errorf("turn %d has empty user text", tr.Index)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/transcript/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/transcript/turn.go`**

```go
// Package transcript turns an agent's conversation into Turns. A turn starts
// at a real user prompt and contains the agent's text and tool calls up to the
// next prompt.
package transcript

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	SourceClaude     = "claude"
	SourceCodex      = "codex"
	SourceScrollback = "scrollback"
)

// MaxFieldChars caps each user/assistant field sent to a dot.
const MaxFieldChars = 8000

type ToolUse struct {
	Name  string
	Paths []string
}

type Turn struct {
	Index     int // 1-based, stable for an append-only transcript
	User      string
	Assistant string
	Tools     []ToolUse
	FromDots  []string // dots whose delivered message is this turn's prompt
}

type Reader interface {
	Turns(ctx context.Context) ([]Turn, error)
	Source() string
}

func (t Turn) TouchedPaths() []string {
	var out []string
	for _, tu := range t.Tools {
		for _, p := range tu.Paths {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func (t Turn) IsFromDot(name string) bool { return slices.Contains(t.FromDots, name) }
func (t Turn) FromAnyDot() bool           { return len(t.FromDots) > 0 }

// DotPrefix is the header delivery puts in front of a dot's message.
func DotPrefix(glyph, name string) string { return fmt.Sprintf("[%s %s dot]", glyph, name) }

var prefixRe = regexp.MustCompile(`(?m)^\[\S+ (.+?) dot\]`)

func FromDots(text string) []string {
	var out []string
	for _, m := range prefixRe.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// clip shortens s to n runes. keepTail keeps the end (conclusions) instead of
// the start.
func clip(s string, n int, keepTail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if keepTail {
		return "…" + string(r[len(r)-n:])
	}
	return string(r[:n]) + "…"
}

func Format(turns []Turn) string {
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "### Turn %d\n", t.Index)
		if t.User != "" {
			fmt.Fprintf(&b, "USER:\n%s\n\n", clip(strings.TrimSpace(t.User), MaxFieldChars, false))
		}
		if t.Assistant != "" {
			fmt.Fprintf(&b, "AGENT:\n%s\n\n", clip(strings.TrimSpace(t.Assistant), MaxFieldChars, true))
		}
		if p := t.TouchedPaths(); len(p) > 0 {
			fmt.Fprintf(&b, "FILES TOUCHED: %s\n\n", strings.Join(p, ", "))
		}
	}
	return strings.TrimSpace(b.String())
}
```

- [ ] **Step 4: Write `internal/transcript/claude.go`**

```go
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
// direct path, or <home>/.claude/projects/*/<id>.jsonl.
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
		matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", value+".jsonl"))
		if len(matches) == 0 {
			return "", fmt.Errorf("no transcript for claude session %s", value)
		}
		return matches[0], nil
	}
	return "", fmt.Errorf("unknown session kind %q", kind)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/transcript/`
Expected: PASS (`TestParseClaudeRealFixture` passes if Task 1 captured the fixture; otherwise SKIP).

- [ ] **Step 6: Commit**

```bash
git add internal/transcript
git commit -m "feat: turn model and Claude Code transcript reader"
```

---

### Task 7: Codex and scrollback readers, reader selection

**Files:**
- Create: `internal/transcript/codex.go`, `internal/transcript/scrollback.go`, `internal/transcript/select.go`
- Test: `internal/transcript/codex_test.go`, `internal/transcript/scrollback_test.go`, `internal/transcript/select_test.go`

**Interfaces:**
- Consumes: `Turn`, `Reader`, `FromDots`, source consts (Task 6).
- Produces:
  - `ParseCodex(r io.Reader) ([]Turn, error)`; `type CodexReader struct{ Path string }`; `ResolveCodexPath(home, kind, value string) (string, error)`
  - `type PaneReader interface{ PaneRead(ctx context.Context, paneID, source string, lines int) (string, error) }` (satisfied by `*herdr.SocketClient`)
  - `NewScrollback(p PaneReader, paneID string, lines int) *ScrollbackReader`
  - `type SelectOptions struct{ Agent, SessionKind, SessionValue string; ForceScrollback bool; Home string; Pane PaneReader; PaneID string; ScrollbackLines int }`; `Select(o SelectOptions) Reader`

Codex's format is built from its documented rollout layout and **is not live-verified** (Codex isn't installed). Keep this note in the code comment.

- [ ] **Step 1: Write the failing tests**

`internal/transcript/codex_test.go`:
```go
package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexSample = `{"timestamp":"2026-10-08T10:00:00Z","type":"session_meta","payload":{"id":"c1","cwd":"/r"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"Fix the API"}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"rg\",\"todo\"]}"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: api/users.go\n@@\n-a\n+b\n*** Add File: api/users_test.go\n*** End Patch"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Patched users.go"}}
{"type":"event_msg","payload":{"type":"user_message","message":"[✓ Test Writer dot] add a test"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Added the test"}}
not json at all
`

func TestParseCodex(t *testing.T) {
	turns, err := ParseCodex(strings.NewReader(codexSample))
	if err != nil || len(turns) != 2 {
		t.Fatalf("turns %+v %v", turns, err)
	}
	if turns[0].User != "Fix the API" || turns[0].Assistant != "Patched users.go" {
		t.Fatalf("turn 1 %+v", turns[0])
	}
	if p := turns[0].TouchedPaths(); len(p) != 2 || p[0] != "api/users.go" || p[1] != "api/users_test.go" {
		t.Fatalf("paths %v", p)
	}
	if !turns[1].IsFromDot("Test Writer") {
		t.Fatalf("turn 2 FromDots %v", turns[1].FromDots)
	}
}

func TestResolveCodexPath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	os.MkdirAll(dir, 0o700)
	f := filepath.Join(dir, "rollout-2026-10-08T10-00-00-c1.jsonl")
	os.WriteFile(f, []byte(codexSample), 0o600)
	if p, err := ResolveCodexPath(home, "id", "c1"); err != nil || p != f {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := ResolveCodexPath(home, "id", "nope"); err == nil {
		t.Fatal("missing must error")
	}
}
```

`internal/transcript/scrollback_test.go`:
```go
package transcript

import (
	"context"
	"testing"
)

type fakePane struct{ text string }

func (f *fakePane) PaneRead(context.Context, string, string, int) (string, error) { return f.text, nil }

func TestScrollbackEmitsDeltaTurns(t *testing.T) {
	p := &fakePane{text: "$ claude\n> hello\nHi there"}
	r := NewScrollback(p, "w1:p1", 400)
	ctx := context.Background()

	turns, _ := r.Turns(ctx)
	if len(turns) != 1 || turns[0].Assistant != "$ claude\n> hello\nHi there" || r.Source() != SourceScrollback {
		t.Fatalf("first read %+v", turns)
	}
	turns, _ = r.Turns(ctx)
	if len(turns) != 1 {
		t.Fatal("unchanged screen must not add a turn")
	}
	p.text = "$ claude\n> hello\nHi there\n> [◆ Security dot] check auth\nDone."
	turns, _ = r.Turns(ctx)
	if len(turns) != 2 || turns[1].Assistant != "> [◆ Security dot] check auth\nDone." || turns[1].Index != 2 {
		t.Fatalf("delta %+v", turns)
	}
}
```

`internal/transcript/select_test.go`:
```go
package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectPrefersTranscriptAndFallsBack(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-r")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(claudeSample), 0o600)
	pane := &fakePane{}

	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "s1", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceClaude {
		t.Fatalf("want claude reader, got %s", r.Source())
	}
	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "missing", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("unresolvable session must fall back to scrollback")
	}
	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "s1", ForceScrollback: true, Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("ForceScrollback must win")
	}
	if r := Select(SelectOptions{Agent: "gemini", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("unsupported agents use scrollback")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/transcript/`
Expected: FAIL — undefined `ParseCodex`, `NewScrollback`, `Select`.

- [ ] **Step 3: Write `internal/transcript/codex.go`**

```go
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
		matches, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+value+".jsonl"))
		if len(matches) == 0 {
			return "", fmt.Errorf("no transcript for codex session %s", value)
		}
		return matches[len(matches)-1], nil
	}
	return "", fmt.Errorf("unknown session kind %q", kind)
}
```

- [ ] **Step 4: Write `internal/transcript/scrollback.go`**

```go
package transcript

import (
	"context"
	"strings"
	"sync"
)

type PaneReader interface {
	PaneRead(ctx context.Context, paneID, source string, lines int) (string, error)
}

// ScrollbackReader is the lossy fallback: each read that changed the screen
// becomes one turn holding the new text since the previous read.
type ScrollbackReader struct {
	pane   PaneReader
	paneID string
	lines  int

	mu    sync.Mutex
	last  string
	turns []Turn
}

func NewScrollback(p PaneReader, paneID string, lines int) *ScrollbackReader {
	return &ScrollbackReader{pane: p, paneID: paneID, lines: lines}
}

func (r *ScrollbackReader) Source() string { return SourceScrollback }

func (r *ScrollbackReader) Turns(ctx context.Context) ([]Turn, error) {
	text, err := r.pane.PaneRead(ctx, r.paneID, "recent_unwrapped", r.lines)
	if err != nil {
		return nil, err
	}
	text = strings.TrimRight(text, " \n")
	r.mu.Lock()
	defer r.mu.Unlock()
	if text != r.last {
		delta := newTail(r.last, text)
		r.last = text
		if strings.TrimSpace(delta) != "" {
			r.turns = append(r.turns, Turn{Index: len(r.turns) + 1, Assistant: delta, FromDots: FromDots(delta)})
		}
	}
	return append([]Turn(nil), r.turns...), nil
}

// newTail returns the part of cur after the last few lines of old.
func newTail(old, cur string) string {
	if old == "" {
		return cur
	}
	lines := strings.Split(old, "\n")
	n := min(5, len(lines))
	anchor := strings.Join(lines[len(lines)-n:], "\n")
	if i := strings.LastIndex(cur, anchor); i >= 0 {
		return strings.TrimLeft(cur[i+len(anchor):], "\n")
	}
	return cur
}
```

- [ ] **Step 5: Write `internal/transcript/select.go`**

```go
package transcript

type SelectOptions struct {
	Agent           string // herdr agent kind: "claude", "codex", ...
	SessionKind     string // "id" | "path"
	SessionValue    string
	ForceScrollback bool
	Home            string
	Pane            PaneReader
	PaneID          string
	ScrollbackLines int // 0 → 400
}

// Select returns the best reader for a pane: the agent's transcript file when
// it can be resolved, otherwise the scrollback reader.
func Select(o SelectOptions) Reader {
	if !o.ForceScrollback && o.SessionValue != "" {
		switch o.Agent {
		case "claude":
			if p, err := ResolveClaudePath(o.Home, o.SessionKind, o.SessionValue); err == nil {
				return ClaudeReader{Path: p}
			}
		case "codex":
			if p, err := ResolveCodexPath(o.Home, o.SessionKind, o.SessionValue); err == nil {
				return CodexReader{Path: p}
			}
		}
	}
	lines := o.ScrollbackLines
	if lines == 0 {
		lines = 400
	}
	return NewScrollback(o.Pane, o.PaneID, lines)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/transcript/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/transcript
git commit -m "feat: codex and scrollback transcript readers with reader selection"
```

---

### Task 8: Backend interface, prompts, verdict parsing and process runner

**Files:**
- Create: `internal/backend/backend.go`, `internal/backend/prompt.go`, `internal/backend/verdict.go`, `internal/backend/exec.go`
- Test: `internal/backend/prompt_test.go`, `internal/backend/verdict_test.go`, `internal/backend/exec_test.go`

**Interfaces:**
- Consumes: `transcript.Turn`, `transcript.Format`.
- Produces:
  - `type Verdict struct{ Relevant bool; Severity, Message string }`
  - `type Request struct{ DotName, Purpose, Verbosity, Model string; Args []string; RepoRead bool; Cwd string; Turns []transcript.Turn; Question string; Force bool }`
  - `type Result struct{ Verdict Verdict; InputTokens, OutputTokens int; CostUSD float64; Raw string }`
  - `type Backend interface{ Kind() string; Check(ctx context.Context) error; Run(ctx context.Context, r Request) (Result, error) }`
  - `SystemPrompt(r Request) string`; `UserPrompt(r Request) string`
  - `ParseVerdict(text string) (Verdict, error)`; `var ErrMalformed`
  - `type Cmd struct{ Name string; Args []string; Dir string; Env []string; Stdin string }`; `type Runner interface{ Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error) }`; `type ExecRunner struct{}`; `CleanEnv(env []string) []string`; `Hint(kind, output string) string`

- [ ] **Step 1: Write the failing tests**

`internal/backend/verdict_test.go`:
```go
package backend

import (
	"errors"
	"testing"
)

// Review Focus 3: tolerate fences/prose and severity synonyms; reject unsafe shapes.
func TestParseVerdict(t *testing.T) {
	ok := []struct {
		in   string
		want Verdict
	}{
		{`{"relevant":false,"severity":"info","message":""}`, Verdict{false, "info", ""}},
		{"```json\n{\"relevant\":true,\"severity\":\"critical\",\"message\":\"XSS\"}\n```", Verdict{true, "critical", "XSS"}},
		{`Here you go: {"relevant": true, "severity": "WARNING", "message": " check  "} thanks`, Verdict{true, "warn", "check"}},
		{`{"relevant":true,"message":"no severity given"}`, Verdict{true, "info", "no severity given"}},
		{`{"relevant":true,"severity":"spicy","message":"x"}`, Verdict{true, "info", "x"}},
	}
	for _, c := range ok {
		got, err := ParseVerdict(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseVerdict(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	bad := []string{
		``,
		`I have nothing to add.`,
		`{"severity":"info","message":"x"}`,       // relevant missing
		`{"relevant":true,"severity":"warn","message":""}`, // relevant but empty
		`{"relevant":"yes","message":"x"}`,        // wrong type
	}
	for _, in := range bad {
		if _, err := ParseVerdict(in); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseVerdict(%q) err = %v, want ErrMalformed", in, err)
		}
	}
}
```

`internal/backend/prompt_test.go`:
```go
package backend

import (
	"strings"
	"testing"

	"herdr-dots/internal/transcript"
)

func TestSystemPromptContainsPurposeAndContract(t *testing.T) {
	s := SystemPrompt(Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "terse"})
	for _, want := range []string{"Security", "Find auth bugs.", `"relevant"`, "relevant\": false", "1-2 sentences"} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt missing %q:\n%s", want, s)
		}
	}
	forced := SystemPrompt(Request{DotName: "Security", Purpose: "p", Verbosity: "normal", Force: true})
	if !strings.Contains(forced, "summoned") {
		t.Errorf("forced prompt must say the dot was summoned:\n%s", forced)
	}
}

func TestUserPromptIncludesTurnsAndQuestion(t *testing.T) {
	u := UserPrompt(Request{Turns: []transcript.Turn{{Index: 3, User: "add login", Assistant: "done"}}, Question: "is the cookie safe?"})
	for _, want := range []string{"### Turn 3", "add login", "QUESTION FROM THE DEVELOPER", "is the cookie safe?"} {
		if !strings.Contains(u, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
}
```

`internal/backend/exec_test.go`:
```go
package backend

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCleanEnvStripsHerdrAndClaudeVars(t *testing.T) {
	in := []string{"PATH=/bin", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH=/s", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "HOME=/h"}
	got := CleanEnv(in)
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/h"}) {
		t.Fatalf("CleanEnv = %v", got)
	}
}

func TestExecRunnerPassesStdinDirEnv(t *testing.T) {
	dir := t.TempDir()
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s|%s|' "$(pwd)" "$DOTS_X"; cat`},
		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=/usr/bin:/bin"}, Stdin: "from-stdin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "|yes|from-stdin") || !strings.Contains(string(out), dir[len(dir)-8:]) {
		t.Fatalf("out = %q", out)
	}
}

func TestExecRunnerKillsProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "sleep 30 & sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v; process group not killed", time.Since(start))
	}
}

func TestHint(t *testing.T) {
	if h := Hint("claude-cli", "Error: Not logged in. Please run /login"); !strings.Contains(h, "claude") || !strings.Contains(h, "/login") {
		t.Fatalf("claude hint %q", h)
	}
	if h := Hint("codex-cli", "401 Unauthorized"); !strings.Contains(h, "codex login") {
		t.Fatalf("codex hint %q", h)
	}
	long := strings.Repeat("e", 1000)
	if h := Hint("claude-cli", long); len(h) > 310 {
		t.Fatalf("hint not truncated: %d", len(h))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backend/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/backend/backend.go`**

```go
// Package backend runs a dot against a model. v1 ships CLI backends (claude,
// codex) that reuse the user's existing logins; API and Ollama backends plug in
// behind the same interface in a later increment.
package backend

import (
	"context"

	"herdr-dots/internal/transcript"
)

type Verdict struct {
	Relevant bool
	Severity string // info | warn | critical
	Message  string
}

type Request struct {
	DotName   string
	Purpose   string
	Verbosity string
	Model     string
	Args      []string
	RepoRead  bool
	Cwd       string
	Turns     []transcript.Turn
	Question  string // manual summon only
	Force     bool   // summoned: must answer
}

type Result struct {
	Verdict      Verdict
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Raw          string
}

type Backend interface {
	Kind() string
	Check(ctx context.Context) error
	Run(ctx context.Context, r Request) (Result, error)
}
```

- [ ] **Step 4: Write `internal/backend/prompt.go`**

```go
package backend

import (
	"fmt"
	"strings"

	"herdr-dots/internal/transcript"
)

const contract = `RESPONSE FORMAT - reply with ONLY one JSON object, no prose, no code fences:
{"relevant": true or false, "severity": "info" | "warn" | "critical", "message": "<your input>"}
Rules:
- You are read-only. Suggest; never claim to have changed anything.
- Never repeat advice that already appears in the conversation.
- "critical" is for security holes, data loss or broken builds; "warn" for real problems; "info" for improvements.`

const silenceRule = `- If you have nothing important to add from your purpose, reply {"relevant": false, "severity": "info", "message": ""}. Silence is better than noise.`

const forceRule = `- The developer summoned you directly. Always answer with "relevant": true and your best input.`

func verbosityRule(v string) string {
	switch v {
	case "terse":
		return "Keep the message to 1-2 sentences."
	case "detailed":
		return "Be thorough: explain the reasoning and give concrete steps or code."
	default:
		return "Keep the message short: a few sentences or a short list."
	}
}

func SystemPrompt(r Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %q, a specialist \"dot\" observing a developer's conversation with a coding agent. Contribute only from your expertise.\n\n", r.DotName)
	fmt.Fprintf(&b, "YOUR PURPOSE:\n%s\n\n", strings.TrimSpace(r.Purpose))
	fmt.Fprintf(&b, "%s\n\n", verbosityRule(r.Verbosity))
	b.WriteString(contract + "\n")
	if r.Force {
		b.WriteString(forceRule + "\n")
	} else {
		b.WriteString(silenceRule + "\n")
	}
	return b.String()
}

func UserPrompt(r Request) string {
	var b strings.Builder
	b.WriteString("CONVERSATION (most recent turns):\n\n")
	b.WriteString(transcript.Format(r.Turns))
	if q := strings.TrimSpace(r.Question); q != "" {
		fmt.Fprintf(&b, "\n\nQUESTION FROM THE DEVELOPER:\n%s", q)
	}
	b.WriteString("\n\nRespond with the JSON object only.")
	return b.String()
}
```

- [ ] **Step 5: Write `internal/backend/verdict.go`**

```go
package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrMalformed = errors.New("malformed verdict")

// ParseVerdict extracts the first {...} span from model output. It tolerates
// code fences and surrounding prose, normalizes severity, and rejects a
// missing "relevant" or a relevant verdict with no message.
func ParseVerdict(text string) (Verdict, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return Verdict{}, fmt.Errorf("%w: no JSON object in %q", ErrMalformed, clipErr(text))
	}
	var raw struct {
		Relevant *bool  `json:"relevant"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return Verdict{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if raw.Relevant == nil {
		return Verdict{}, fmt.Errorf("%w: missing \"relevant\"", ErrMalformed)
	}
	v := Verdict{Relevant: *raw.Relevant, Message: strings.TrimSpace(raw.Message)}
	switch strings.ToLower(strings.TrimSpace(raw.Severity)) {
	case "critical":
		v.Severity = "critical"
	case "warn", "warning":
		v.Severity = "warn"
	default:
		v.Severity = "info"
	}
	if v.Relevant && v.Message == "" {
		return Verdict{}, fmt.Errorf("%w: relevant verdict with empty message", ErrMalformed)
	}
	return v, nil
}

func clipErr(s string) string {
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}
```

- [ ] **Step 6: Write `internal/backend/exec.go`**

```go
package backend

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"syscall"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)
}

// ExecRunner runs commands in their own process group so a timeout kills the
// CLI and everything it spawned.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	cmd.Stdin = strings.NewReader(c.Stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), errb.Bytes(), err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return out.Bytes(), errb.Bytes(), ctx.Err()
	}
}

// CleanEnv drops variables that would make a dot's CLI talk to Herdr or think
// it runs nested inside another Claude Code session.
func CleanEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HERDR_") || k == "CLAUDECODE" || k == "CLAUDE_CODE_ENTRYPOINT" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Hint turns CLI failure output into a fix the user can act on.
func Hint(kind, output string) string {
	low := strings.ToLower(output)
	if strings.Contains(low, "not logged in") || strings.Contains(low, "login") || strings.Contains(low, "unauthorized") || strings.Contains(low, "401") || strings.Contains(low, "authenticat") {
		if kind == "codex-cli" {
			return "codex is not logged in: run `codex login`"
		}
		return "claude is not logged in: run `claude` and type /login"
	}
	out := strings.TrimSpace(output)
	if r := []rune(out); len(r) > 300 {
		out = string(r[:300]) + "…"
	}
	if out == "" {
		out = "the CLI exited with an error and no output"
	}
	return out
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -race ./internal/backend/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/backend
git commit -m "feat: backend interface, dot prompts, tolerant verdict parsing, process-group runner"
```

---

### Task 9: claude-cli and codex-cli backends

**Files:**
- Create: `internal/backend/claude.go`, `internal/backend/codex.go`, `internal/backend/registry.go`
- Test: `internal/backend/claude_test.go`, `internal/backend/codex_test.go`, `internal/backend/live_test.go`

**Interfaces:**
- Consumes: `Request`, `Result`, `Cmd`, `Runner`, `CleanEnv`, `Hint`, `SystemPrompt`, `UserPrompt`, `ParseVerdict` (Task 8).
- Produces:
  - `type ClaudeCLI struct{ Runner Runner; Bin string; Environ func() []string }` with `Kind()`, `Check(ctx)`, `Run(ctx, Request)`, `BuildCmd(Request) Cmd`; `ParseClaudeOutput(out []byte) (Result, error)`
  - `type CodexCLI struct{ Runner Runner; Bin string; Environ func() []string }` with the same methods; `ParseCodexOutput(out []byte) (Result, error)`
  - `New(kind string, r Runner) (Backend, error)`

- [ ] **Step 1: Write the failing tests**

`internal/backend/claude_test.go`:
```go
package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/transcript"
)

type fakeRunner struct {
	got    Cmd
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, c Cmd) ([]byte, []byte, error) {
	f.got = c
	return []byte(f.stdout), []byte(f.stderr), f.err
}

func req() Request {
	return Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "normal", Model: "sonnet", RepoRead: true, Cwd: "/repo",
		Turns: []transcript.Turn{{Index: 1, User: "add login", Assistant: "done"}}}
}

func argAfter(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func TestClaudeBuildCmdIsReadOnlyAndHookless(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return []string{"PATH=/bin", "HERDR_PANE_ID=x"} }}
	cmd := c.BuildCmd(req())
	if cmd.Name != "claude" || cmd.Dir != "/repo" {
		t.Fatalf("cmd %+v", cmd)
	}
	for _, f := range []string{"-p", "--no-session-persistence", "--strict-mcp-config"} {
		if !slices.Contains(cmd.Args, f) {
			t.Errorf("missing %s", f)
		}
	}
	if v, _ := argAfter(cmd.Args, "--output-format"); v != "json" {
		t.Errorf("output-format %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--settings"); v != `{"disableAllHooks":true}` {
		t.Errorf("settings %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--tools"); v != "Read,Grep,Glob" {
		t.Errorf("tools %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--model"); v != "sonnet" {
		t.Errorf("model %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--append-system-prompt"); !strings.Contains(v, "Find auth bugs.") {
		t.Errorf("system prompt not passed")
	}
	for _, banned := range []string{"--bare", "--dangerously-skip-permissions", "bypassPermissions"} {
		if slices.Contains(cmd.Args, banned) {
			t.Errorf("must never pass %s", banned)
		}
	}
	if slices.Contains(cmd.Env, "HERDR_PANE_ID=x") {
		t.Error("HERDR_ vars must be stripped")
	}
	if !strings.Contains(cmd.Stdin, "add login") {
		t.Error("turns go on stdin")
	}
	noRepo := req()
	noRepo.RepoRead = false
	if v, ok := argAfter(c.BuildCmd(noRepo).Args, "--tools"); !ok || v != "" {
		t.Errorf("without repo_read tools must be empty, got %q", v)
	}
}

func TestParseClaudeOutput(t *testing.T) {
	out := `{"type":"result","is_error":false,"result":"{\"relevant\":true,\"severity\":\"warn\",\"message\":\"Use httpOnly\"}","usage":{"input_tokens":120,"output_tokens":30},"total_cost_usd":0.01}`
	res, err := ParseClaudeOutput([]byte(out))
	if err != nil || !res.Verdict.Relevant || res.Verdict.Message != "Use httpOnly" || res.InputTokens != 120 || res.OutputTokens != 30 || res.CostUSD != 0.01 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := ParseClaudeOutput([]byte(`{"type":"result","is_error":true,"result":"API overloaded"}`)); err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("is_error must surface: %v", err)
	}
	if _, err := ParseClaudeOutput([]byte(`not json`)); err == nil {
		t.Fatal("non-JSON must error")
	}
}

func TestClaudeRunMapsLoginFailureToHint(t *testing.T) {
	r := &fakeRunner{stdout: "", stderr: "Invalid API key · Please run /login", err: errors.New("exit status 1")}
	_, err := ClaudeCLI{Runner: r, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "/login") {
		t.Fatalf("want login hint, got %v", err)
	}
}

func TestClaudeCheckMissingBinary(t *testing.T) {
	err := ClaudeCLI{Bin: "definitely-not-a-real-binary-xyz"}.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Check = %v", err)
	}
}
```

`internal/backend/codex_test.go`:
```go
package backend

import (
	"slices"
	"strings"
	"testing"
)

func TestCodexBuildCmd(t *testing.T) {
	cmd := CodexCLI{Environ: func() []string { return []string{"HERDR_ENV=1", "PATH=/bin"} }}.BuildCmd(req())
	if cmd.Name != "codex" || cmd.Args[0] != "exec" || cmd.Args[len(cmd.Args)-1] != "-" {
		t.Fatalf("args %v", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "--sandbox"); v != "read-only" {
		t.Errorf("sandbox %q", v)
	}
	if v, _ := argAfter(cmd.Args, "-C"); v != "/repo" {
		t.Errorf("-C %q", v)
	}
	if !slices.Contains(cmd.Args, "--json") || !slices.Contains(cmd.Args, "--skip-git-repo-check") {
		t.Errorf("missing flags: %v", cmd.Args)
	}
	if slices.Contains(cmd.Env, "HERDR_ENV=1") {
		t.Error("env not cleaned")
	}
	if !strings.Contains(cmd.Stdin, "Find auth bugs.") || !strings.Contains(cmd.Stdin, "add login") {
		t.Error("codex gets system + user prompt on stdin")
	}
}

func TestParseCodexOutput(t *testing.T) {
	out := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t"}`,
		`{"type":"item.completed","item":{"id":"i0","type":"reasoning","text":"thinking"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"{\"relevant\":true,\"severity\":\"critical\",\"message\":\"SQL injection in users.go\"}"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":900,"cached_input_tokens":100,"output_tokens":40}}`,
	}, "\n")
	res, err := ParseCodexOutput([]byte(out))
	if err != nil || res.Verdict.Severity != "critical" || res.InputTokens != 900 || res.OutputTokens != 40 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := ParseCodexOutput([]byte(`{"type":"turn.failed","error":{"message":"usage limit"}}`)); err == nil || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("turn.failed must surface: %v", err)
	}
}

func TestRegistry(t *testing.T) {
	for _, k := range []string{"claude-cli", "codex-cli"} {
		b, err := New(k, &fakeRunner{})
		if err != nil || b.Kind() != k {
			t.Fatalf("%s: %v", k, err)
		}
	}
	if _, err := New("gpt-web", nil); err == nil {
		t.Fatal("unknown kind must error")
	}
}
```

`internal/backend/live_test.go`:
```go
//go:build live

package backend

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Run with: go test -tags live ./internal/backend/ -run Live -v
func TestLiveClaude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	r := req()
	r.Model = "haiku"
	r.RepoRead = false
	r.Cwd = t.TempDir()
	res, err := New("claude-cli", ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.Run(ctx, r)
	if err != nil {
		t.Fatalf("live claude: %v", err)
	}
	t.Logf("verdict %+v tokens %d/%d", out.Verdict, out.InputTokens, out.OutputTokens)
}

func TestLiveCodex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	r := req()
	r.Model = ""
	r.Cwd = t.TempDir()
	b, _ := New("codex-cli", ExecRunner{})
	out, err := b.Run(ctx, r)
	if err != nil {
		t.Fatalf("live codex: %v", err)
	}
	t.Logf("verdict %+v", out.Verdict)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/backend/`
Expected: FAIL — undefined `ClaudeCLI`, `CodexCLI`, `New`.

- [ ] **Step 3: Write `internal/backend/claude.go`**

```go
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type ClaudeCLI struct {
	Runner  Runner
	Bin     string          // default "claude"
	Environ func() []string // default os.Environ
}

func (c ClaudeCLI) Kind() string { return "claude-cli" }

func (c ClaudeCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "claude"
}

func (c ClaudeCLI) environ() []string {
	if c.Environ != nil {
		return c.Environ()
	}
	return os.Environ()
}

func (c ClaudeCLI) Check(context.Context) error {
	if _, err := exec.LookPath(c.bin()); err != nil {
		return fmt.Errorf("claude CLI not found on PATH: install Claude Code (https://claude.com/claude-code)")
	}
	return nil
}

// BuildCmd: print mode, JSON output, no saved session, no MCP servers, hooks
// disabled (so Herdr's integration hooks never fire for a dot), read-only tools.
func (c ClaudeCLI) BuildCmd(r Request) Cmd {
	args := []string{
		"-p", "--output-format", "json",
		"--no-session-persistence",
		"--strict-mcp-config",
		"--settings", `{"disableAllHooks":true}`,
		"--append-system-prompt", SystemPrompt(r),
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if r.RepoRead {
		args = append(args, "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob")
	} else {
		args = append(args, "--tools", "")
	}
	args = append(args, r.Args...)
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r)}
}

func (c ClaudeCLI) Run(ctx context.Context, r Request) (Result, error) {
	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("claude timed out: %w", ctx.Err())
	}
	res, perr := ParseClaudeOutput(out)
	if err != nil && perr != nil {
		return Result{}, fmt.Errorf("claude failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
	}
	return res, perr
}

func ParseClaudeOutput(out []byte) (Result, error) {
	var o struct {
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Usage   struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &o); err != nil {
		return Result{}, fmt.Errorf("claude output is not JSON: %w", err)
	}
	if o.IsError {
		return Result{}, fmt.Errorf("claude error: %s", o.Result)
	}
	res := Result{InputTokens: o.Usage.In, OutputTokens: o.Usage.Out, CostUSD: o.Cost, Raw: o.Result}
	v, err := ParseVerdict(o.Result)
	res.Verdict = v
	return res, err
}
```

- [ ] **Step 4: Write `internal/backend/codex.go`**

```go
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// CodexCLI runs `codex exec --json` in a read-only sandbox. Codex has no
// system-prompt flag, so system and user prompts go together on stdin.
// NOTE: output format from Codex's docs; not yet live-verified (V4).
type CodexCLI struct {
	Runner  Runner
	Bin     string
	Environ func() []string
}

func (c CodexCLI) Kind() string { return "codex-cli" }

func (c CodexCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "codex"
}

func (c CodexCLI) environ() []string {
	if c.Environ != nil {
		return c.Environ()
	}
	return os.Environ()
}

func (c CodexCLI) Check(context.Context) error {
	if _, err := exec.LookPath(c.bin()); err != nil {
		return fmt.Errorf("codex CLI not found on PATH: install it with `npm i -g @openai/codex`")
	}
	return nil
}

func (c CodexCLI) BuildCmd(r Request) Cmd {
	args := []string{"exec", "--json", "--sandbox", "read-only", "--skip-git-repo-check"}
	if r.Cwd != "" {
		args = append(args, "-C", r.Cwd)
	}
	if r.Model != "" {
		args = append(args, "-m", r.Model)
	}
	args = append(args, r.Args...)
	args = append(args, "-")
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: SystemPrompt(r) + "\n\n" + UserPrompt(r)}
}

func (c CodexCLI) Run(ctx context.Context, r Request) (Result, error) {
	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("codex timed out: %w", ctx.Err())
	}
	res, perr := ParseCodexOutput(out)
	if err != nil && perr != nil {
		return Result{}, fmt.Errorf("codex failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
	}
	return res, perr
}

func ParseCodexOutput(out []byte) (Result, error) {
	var res Result
	var last, failure string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Item    struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				In  int `json:"input_tokens"`
				Out int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item.Type == "agent_message" {
				last = ev.Item.Text
			}
		case "turn.completed":
			res.InputTokens, res.OutputTokens = ev.Usage.In, ev.Usage.Out
		case "turn.failed", "error":
			failure = ev.Error.Message
			if failure == "" {
				failure = ev.Message
			}
		}
	}
	if last == "" {
		if failure != "" {
			return res, fmt.Errorf("codex error: %s", failure)
		}
		return res, errors.New("codex produced no agent message")
	}
	res.Raw = last
	v, err := ParseVerdict(last)
	res.Verdict = v
	return res, err
}
```

- [ ] **Step 5: Write `internal/backend/registry.go`**

```go
package backend

import "fmt"

// New returns the backend for a dot's [backend].kind.
func New(kind string, r Runner) (Backend, error) {
	switch kind {
	case "claude-cli":
		return ClaudeCLI{Runner: r}, nil
	case "codex-cli":
		return CodexCLI{Runner: r}, nil
	}
	return nil, fmt.Errorf("unknown backend %q", kind)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/backend/ && go test -tags live ./internal/backend/ -run LiveClaude -v`
Expected: unit tests PASS; the live test PASSES (prints a verdict) on a machine with a logged-in `claude`, and skips otherwise.

- [ ] **Step 7: Commit**

```bash
git add internal/backend
git commit -m "feat: claude-cli and codex-cli backends with live tests behind -tags live"
```

---

### Task 10: Card queue with persistence and a state machine

**Files:**
- Create: `internal/queue/queue.go`
- Test: `internal/queue/queue_test.go`

**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`.
- Produces:
  - `type Status string`; consts `StatusPending`, `StatusAuto`, `StatusSent`, `StatusDismissed`, `StatusOrphaned`
  - `type Card struct{ ID, Dot, DotName, Glyph, PaneID string; Turn int; Severity, Body, Mode string; Approved bool; Status Status; Error string; CreatedAt time.Time }` — `Dot` is the slug, `Mode` the delivery mode string the card was created under (`"suggest"` cards cannot be sent).
  - `Open(path string) (*Store, error)`; methods `Add(Card) (Card, error)`, `Get(id string) (Card, bool)`, `Pending(paneID string) []Card` (pending+auto), `Visible(paneID string) []Card` (pending+auto+orphaned), `All() []Card`, `SetStatus(id string, st Status) error`, `Approve(id string) error`, `Edit(id, body string) error`, `Fail(id, msg string) error`, `OrphanPane(paneID string) (int, error)`
  - `var ErrNotFound, ErrTransition`

- [ ] **Step 1: Write the failing tests**

`internal/queue/queue_test.go`:
```go
package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	p := filepath.Join(t.TempDir(), "queue.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestAddAssignsIDAndPersists(t *testing.T) {
	s, p := open(t)
	c, err := s.Add(Card{Dot: "security", PaneID: "w1:p1", Severity: "warn", Body: "x", Mode: "queue"})
	if err != nil || c.ID == "" || c.Status != StatusPending || c.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", c, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	s2, _ := Open(p)
	if got, ok := s2.Get(c.ID); !ok || got.Body != "x" {
		t.Fatal("not persisted")
	}
	c2, _ := s2.Add(Card{PaneID: "w1:p1", Body: "y"})
	if c2.ID == c.ID {
		t.Fatal("ids must stay unique across reopen")
	}
}

func TestPendingOrdersBySeverityThenAge(t *testing.T) {
	s, _ := open(t)
	a, _ := s.Add(Card{PaneID: "p", Severity: "info", Body: "a"})
	b, _ := s.Add(Card{PaneID: "p", Severity: "critical", Body: "b"})
	c, _ := s.Add(Card{PaneID: "p", Severity: "warn", Body: "c", Status: StatusAuto})
	_, _ = s.Add(Card{PaneID: "other", Severity: "critical", Body: "d"})
	got := s.Pending("p")
	if len(got) != 3 || got[0].ID != b.ID || got[1].ID != c.ID || got[2].ID != a.ID {
		t.Fatalf("order %+v", got)
	}
}

func TestTransitions(t *testing.T) {
	s, _ := open(t)
	c, _ := s.Add(Card{PaneID: "p", Body: "x"})
	if err := s.Approve(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(c.ID, StatusAuto); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(c.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Status != StatusPending || got.Error != "boom" || !got.Approved {
		t.Fatalf("after fail %+v", got)
	}
	if err := s.SetStatus(c.ID, StatusSent); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(c.ID)
	if got.Error != "" {
		t.Fatal("sent clears error")
	}
	if err := s.SetStatus(c.ID, StatusPending); !errors.Is(err, ErrTransition) {
		t.Fatalf("sent is terminal, got %v", err)
	}
	if err := s.Edit(c.ID, "new"); !errors.Is(err, ErrTransition) {
		t.Fatalf("cannot edit sent card, got %v", err)
	}
	if err := s.SetStatus("nope", StatusSent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestEditRejectsEmpty(t *testing.T) {
	s, _ := open(t)
	c, _ := s.Add(Card{PaneID: "p", Body: "x"})
	if err := s.Edit(c.ID, "   "); err == nil {
		t.Fatal("empty body must be rejected")
	}
	if err := s.Edit(c.ID, " better "); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Body != "better" {
		t.Fatalf("body %q", got.Body)
	}
}

func TestOrphanPane(t *testing.T) {
	s, _ := open(t)
	a, _ := s.Add(Card{PaneID: "p", Body: "a"})
	b, _ := s.Add(Card{PaneID: "p", Body: "b"})
	_ = s.SetStatus(b.ID, StatusDismissed)
	n, err := s.OrphanPane("p")
	if err != nil || n != 1 {
		t.Fatalf("orphaned %d %v", n, err)
	}
	if len(s.Pending("p")) != 0 || len(s.Visible("p")) != 1 || s.Visible("p")[0].ID != a.ID {
		t.Fatal("orphaned cards are visible but not pending")
	}
	if err := s.SetStatus(a.ID, StatusDismissed); err != nil {
		t.Fatal("orphaned cards can be dismissed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/queue/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/queue/queue.go`**

```go
// Package queue stores dot messages (cards) waiting for the developer, and
// enforces how a card may move between states.
package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
)

type Status string

const (
	StatusPending   Status = "pending"   // waiting for the developer
	StatusAuto      Status = "auto"      // will be delivered automatically when allowed
	StatusSent      Status = "sent"      // delivered (terminal)
	StatusDismissed Status = "dismissed" // thrown away (terminal)
	StatusOrphaned  Status = "orphaned"  // its main pane closed; viewable, dismissable
)

var (
	ErrNotFound   = errors.New("card not found")
	ErrTransition = errors.New("invalid card transition")
)

var allowed = map[Status][]Status{
	StatusPending:  {StatusSent, StatusDismissed, StatusAuto, StatusOrphaned},
	StatusAuto:     {StatusSent, StatusPending, StatusDismissed, StatusOrphaned},
	StatusOrphaned: {StatusDismissed},
}

type Card struct {
	ID        string    `json:"id"`
	Dot       string    `json:"dot"`
	DotName   string    `json:"dot_name"`
	Glyph     string    `json:"glyph"`
	PaneID    string    `json:"pane_id"`
	Turn      int       `json:"turn"`
	Severity  string    `json:"severity"`
	Body      string    `json:"body"`
	Mode      string    `json:"mode"`
	Approved  bool      `json:"approved"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// keepTerminal bounds the file: only the most recent sent/dismissed cards stay.
const keepTerminal = 200

type Store struct {
	path  string
	mu    sync.Mutex
	cards []Card
	next  int
}

type file struct {
	Next  int    `json:"next"`
	Cards []Card `json:"cards"`
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("queue %s: %w", path, err)
	}
	s.cards, s.next = f.Cards, f.Next
	return s, nil
}

func (s *Store) save() error {
	var terminal, live []Card
	for _, c := range s.cards {
		if c.Status == StatusSent || c.Status == StatusDismissed {
			terminal = append(terminal, c)
		} else {
			live = append(live, c)
		}
	}
	if len(terminal) > keepTerminal {
		terminal = terminal[len(terminal)-keepTerminal:]
	}
	s.cards = append(live, terminal...)
	data, err := json.MarshalIndent(file{Next: s.next, Cards: s.cards}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, data, 0o600)
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.cards, func(c Card) bool { return c.ID == id })
}

func (s *Store) Add(c Card) (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	c.ID = strconv.Itoa(s.next)
	c.CreatedAt = time.Now()
	if c.Status == "" {
		c.Status = StatusPending
	}
	s.cards = append(s.cards, c)
	return c, s.save()
}

func (s *Store) Get(id string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(id); i >= 0 {
		return s.cards[i], true
	}
	return Card{}, false
}

func rank(sev string) int {
	switch sev {
	case "critical":
		return 3
	case "warn":
		return 2
	}
	return 1
}

func (s *Store) filter(paneID string, sts ...Status) []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Card
	for _, c := range s.cards {
		if c.PaneID == paneID && slices.Contains(sts, c.Status) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i].Severity) != rank(out[j].Severity) {
			return rank(out[i].Severity) > rank(out[j].Severity)
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Store) Pending(paneID string) []Card { return s.filter(paneID, StatusPending, StatusAuto) }
func (s *Store) Visible(paneID string) []Card {
	return s.filter(paneID, StatusPending, StatusAuto, StatusOrphaned)
}

func (s *Store) All() []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Card(nil), s.cards...)
}

func (s *Store) mutate(id string, fn func(c *Card) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c := s.cards[i]
	if err := fn(&c); err != nil {
		return err
	}
	s.cards[i] = c
	return s.save()
}

func live(c *Card) error {
	if c.Status != StatusPending && c.Status != StatusAuto {
		return fmt.Errorf("%w: card %s is %s", ErrTransition, c.ID, c.Status)
	}
	return nil
}

func (s *Store) SetStatus(id string, st Status) error {
	return s.mutate(id, func(c *Card) error {
		if !slices.Contains(allowed[c.Status], st) {
			return fmt.Errorf("%w: %s → %s", ErrTransition, c.Status, st)
		}
		c.Status = st
		if st == StatusSent {
			c.Error = ""
		}
		return nil
	})
}

func (s *Store) Approve(id string) error {
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Approved = true
		return nil
	})
}

func (s *Store) Edit(id, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("card body cannot be empty")
	}
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Body = body
		return nil
	})
}

// Fail returns a card to pending with an error marker after a failed delivery.
func (s *Store) Fail(id, msg string) error {
	return s.mutate(id, func(c *Card) error {
		if err := live(c); err != nil {
			return err
		}
		c.Status, c.Error = StatusPending, msg
		return nil
	})
}

func (s *Store) OrphanPane(paneID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.cards {
		c := &s.cards[i]
		if c.PaneID == paneID && (c.Status == StatusPending || c.Status == StatusAuto) {
			c.Status = StatusOrphaned
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.save()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/queue/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue
git commit -m "feat: persistent card queue with explicit state transitions"
```

---

### Task 11: Attachments, usage and feed stores

**Files:**
- Create: `internal/state/attachments.go`, `internal/state/usage.go`, `internal/state/feed.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`.
- Produces:
  - `type DotAttachment struct{ Slug string; LastSeenTurn int; Paused bool; CooldownLeft int; Failures int }`
  - `type Attachment struct{ PaneID, Agent, SessionKind, SessionValue, Cwd, Source string; ForceScrollback bool; Dots []DotAttachment }`; `(*Attachment) Find(slug string) *DotAttachment`
  - `OpenAttachments(path string) (*Attachments, error)`; methods `Get(paneID) (Attachment, bool)` (deep copy), `List() []Attachment` (sorted by PaneID), `Put(Attachment) error`, `Delete(paneID string) error`, `Update(paneID string, fn func(*Attachment)) error`; `var ErrNoAttachment`
  - `OpenUsage(path string) (*Usage, error)`; methods `Record(paneID, slug string, in, out int) error`, `Calls(paneID, slug string) int`, `PaneCalls(paneID string) int`, `ResetPane(paneID string) error`
  - Outcome consts `OutcomeFlagged`, `OutcomePass`, `OutcomeError`, `OutcomeBudget`; `type FeedEntry struct{ Time time.Time; PaneID, Dot string; Turn int; Outcome, Detail string }`; `OpenFeed(path string) (*Feed, error)`; methods `Append(FeedEntry) error`, `Recent(paneID string, n int) []FeedEntry` (oldest first)

- [ ] **Step 1: Write the failing tests**

`internal/state/state_test.go`:
```go
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentsCRUDAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "attachments.json")
	s, err := OpenAttachments(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put(Attachment{PaneID: "w1:p2", Agent: "codex"})
	_ = s.Put(Attachment{PaneID: "w1:p1", Agent: "claude", Dots: []DotAttachment{{Slug: "security"}}})
	if l := s.List(); len(l) != 2 || l[0].PaneID != "w1:p1" {
		t.Fatalf("list %+v", l)
	}
	err = s.Update("w1:p1", func(a *Attachment) { a.Find("security").LastSeenTurn = 7 })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update("nope", func(*Attachment) {}); !errors.Is(err, ErrNoAttachment) {
		t.Fatalf("want ErrNoAttachment, got %v", err)
	}
	got, _ := s.Get("w1:p1")
	got.Dots[0].LastSeenTurn = 99 // mutating a copy must not leak into the store
	again, _ := s.Get("w1:p1")
	if again.Dots[0].LastSeenTurn != 7 {
		t.Fatalf("Get must return a deep copy, got %d", again.Dots[0].LastSeenTurn)
	}
	s2, _ := OpenAttachments(p)
	if a, ok := s2.Get("w1:p1"); !ok || a.Find("security").LastSeenTurn != 7 {
		t.Fatal("not persisted")
	}
	_ = s2.Delete("w1:p1")
	if _, ok := s2.Get("w1:p1"); ok {
		t.Fatal("delete")
	}
	if (&Attachment{}).Find("x") != nil {
		t.Fatal("Find on missing slug must be nil")
	}
}

func TestUsage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	u, _ := OpenUsage(p)
	_ = u.Record("p1", "security", 100, 10)
	_ = u.Record("p1", "security", 50, 5)
	_ = u.Record("p1", "frontend", 1, 1)
	if u.Calls("p1", "security") != 2 || u.PaneCalls("p1") != 3 || u.Calls("p2", "x") != 0 {
		t.Fatal("counts")
	}
	u2, _ := OpenUsage(p)
	if u2.PaneCalls("p1") != 3 {
		t.Fatal("not persisted")
	}
	_ = u2.ResetPane("p1")
	if u2.PaneCalls("p1") != 0 {
		t.Fatal("reset")
	}
}

func TestFeedAppendRecentAndReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	f, err := OpenFeed(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 60; i++ {
		_ = f.Append(FeedEntry{Time: time.Now(), PaneID: "p1", Dot: "security", Turn: i, Outcome: OutcomePass})
	}
	_ = f.Append(FeedEntry{PaneID: "p2", Dot: "x", Outcome: OutcomeError, Detail: "boom"})
	r := f.Recent("p1", 5)
	if len(r) != 5 || r[4].Turn != 60 || r[0].Turn != 56 {
		t.Fatalf("recent %+v", r)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	f2, _ := OpenFeed(p)
	if r := f2.Recent("p1", 100); len(r) != 50 || r[49].Turn != 60 {
		t.Fatalf("reload keeps last 50 per pane, got %d", len(r))
	}
	if r := f2.Recent("p2", 10); len(r) != 1 || r[0].Detail != "boom" {
		t.Fatal(fmt.Sprint("p2 ", r))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/state/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/state/attachments.go`**

```go
// Package state holds the engine's persisted runtime state: which dots are
// attached to which main panes, call usage, and the run outcome feed.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"sync"

	"herdr-dots/internal/fsutil"
)

var ErrNoAttachment = errors.New("no dots attached to pane")

type DotAttachment struct {
	Slug         string `json:"slug"`
	LastSeenTurn int    `json:"last_seen_turn"`
	Paused       bool   `json:"paused"`
	CooldownLeft int    `json:"cooldown_left"`
	Failures     int    `json:"failures"`
}

type Attachment struct {
	PaneID          string          `json:"pane_id"`
	Agent           string          `json:"agent"`
	SessionKind     string          `json:"session_kind"`
	SessionValue    string          `json:"session_value"`
	Cwd             string          `json:"cwd"`
	Source          string          `json:"source"`
	ForceScrollback bool            `json:"force_scrollback"`
	Dots            []DotAttachment `json:"dots"`
}

func (a *Attachment) Find(slug string) *DotAttachment {
	for i := range a.Dots {
		if a.Dots[i].Slug == slug {
			return &a.Dots[i]
		}
	}
	return nil
}

func (a Attachment) clone() Attachment {
	a.Dots = append([]DotAttachment(nil), a.Dots...)
	return a
}

type Attachments struct {
	path string
	mu   sync.Mutex
	m    map[string]Attachment
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

func OpenAttachments(path string) (*Attachments, error) {
	s := &Attachments{path: path, m: map[string]Attachment{}}
	if err := readJSON(path, &s.m); err != nil {
		return nil, err
	}
	if s.m == nil {
		s.m = map[string]Attachment{}
	}
	return s, nil
}

func (s *Attachments) Get(paneID string) (Attachment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[paneID]
	return a.clone(), ok
}

func (s *Attachments) List() []Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Attachment, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PaneID < out[j].PaneID })
	return out
}

func (s *Attachments) Put(a Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.PaneID] = a.clone()
	return writeJSON(s.path, s.m)
}

func (s *Attachments) Delete(paneID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, paneID)
	return writeJSON(s.path, s.m)
}

// Update applies fn to the stored attachment and saves. fn must not call back
// into the store.
func (s *Attachments) Update(paneID string, fn func(*Attachment)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[paneID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoAttachment, paneID)
	}
	a = a.clone()
	fn(&a)
	s.m[paneID] = a
	return writeJSON(s.path, s.m)
}
```

- [ ] **Step 4: Write `internal/state/usage.go`**

```go
package state

import "sync"

type Counter struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Usage counts backend calls per main pane and dot. Budgets are per pane.
type Usage struct {
	path string
	mu   sync.Mutex
	data map[string]map[string]*Counter
}

func OpenUsage(path string) (*Usage, error) {
	u := &Usage{path: path, data: map[string]map[string]*Counter{}}
	if err := readJSON(path, &u.data); err != nil {
		return nil, err
	}
	if u.data == nil {
		u.data = map[string]map[string]*Counter{}
	}
	return u, nil
}

func (u *Usage) Record(paneID, slug string, in, out int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.data[paneID] == nil {
		u.data[paneID] = map[string]*Counter{}
	}
	c := u.data[paneID][slug]
	if c == nil {
		c = &Counter{}
		u.data[paneID][slug] = c
	}
	c.Calls++
	c.InputTokens += in
	c.OutputTokens += out
	return writeJSON(u.path, u.data)
}

func (u *Usage) Calls(paneID, slug string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c := u.data[paneID][slug]; c != nil {
		return c.Calls
	}
	return 0
}

func (u *Usage) PaneCalls(paneID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, c := range u.data[paneID] {
		n += c.Calls
	}
	return n
}

func (u *Usage) ResetPane(paneID string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.data, paneID)
	return writeJSON(u.path, u.data)
}
```

- [ ] **Step 5: Write `internal/state/feed.go`**

```go
package state

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	OutcomeFlagged = "flagged"
	OutcomePass    = "pass"
	OutcomeError   = "error"
	OutcomeBudget  = "budget"
)

// feedKeep is how many recent entries per pane stay in memory.
const feedKeep = 50

// feedRotateBytes rotates feed.jsonl to feed.jsonl.1 at open time.
const feedRotateBytes = 5 << 20

type FeedEntry struct {
	Time    time.Time `json:"time"`
	PaneID  string    `json:"pane_id"`
	Dot     string    `json:"dot"`
	Turn    int       `json:"turn"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
}

type Feed struct {
	path   string
	mu     sync.Mutex
	recent map[string][]FeedEntry
}

func OpenFeed(path string) (*Feed, error) {
	f := &Feed{path: path, recent: map[string][]FeedEntry{}}
	if st, err := os.Stat(path); err == nil && st.Size() > feedRotateBytes {
		_ = os.Rename(path, path+".1")
	}
	file, err := os.Open(path)
	if err == nil {
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			var e FeedEntry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				f.remember(e)
			}
		}
		file.Close()
	}
	return f, nil
}

func (f *Feed) remember(e FeedEntry) {
	list := append(f.recent[e.PaneID], e)
	if len(list) > feedKeep {
		list = list[len(list)-feedKeep:]
	}
	f.recent[e.PaneID] = list
}

func (f *Feed) Append(e FeedEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remember(e)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}

// Recent returns up to n entries for a pane, oldest first.
func (f *Feed) Recent(paneID string, n int) []FeedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.recent[paneID]
	if len(list) > n {
		list = list[len(list)-n:]
	}
	return append([]FeedEntry(nil), list...)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/state/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/state
git commit -m "feat: persisted attachments, usage counters and run feed"
```

---

### Task 12: Snapshot view types and the trigger filter

**Files:**
- Create: `internal/view/view.go`, `internal/engine/filter.go`
- Test: `internal/view/view_test.go`, `internal/engine/filter_test.go`

**Interfaces:**
- Consumes: `queue.Card`, `state.FeedEntry`, `state.DotAttachment`, `dots.Dot`, `transcript.Turn`.
- Produces:
  - `view.DotState{Slug, Name, Glyph, Color, Mode, Status string; Paused bool; Calls, Budget int}`; `view.Pane{PaneID, Agent, Source string; Calls, Budget int; Dots []DotState; Cards []queue.Card; Feed []state.FeedEntry}`; `view.LibraryDot{Slug, Name, Glyph, Color, Backend, Model, Mode string}`; `view.Invalid{File string; Line int; Error string}`; `view.Snapshot{Panes []Pane; Library []LibraryDot; Invalid []Invalid; InjectEnabled, PausedAll bool}`; `(Snapshot) Pane(id string) (Pane, bool)`
  - Dot status consts in `view`: `StatusIdle = "idle"`, `StatusThinking = "thinking"`, `StatusPassed = "passed"`, `StatusFlagged = "flagged"`, `StatusError = "error"`, `StatusBudget = "budget"`, `StatusPaused = "paused"`
  - `engine.Reason` with consts `ReasonRun`, `ReasonPausedAll`, `ReasonPaused`, `ReasonObserve`, `ReasonOff`, `ReasonFromDot`, `ReasonCooldown`, `ReasonGlobs`, `ReasonBudget`, `ReasonSessionBudget`
  - `engine.FilterInput{Dot dots.Dot; Att state.DotAttachment; NewTurns []transcript.Turn; PausedAll bool; DotCalls, PaneCalls, PaneBudget int}`; `engine.Decision{Run bool; Reason Reason; Turns []transcript.Turn}`; `engine.Decide(FilterInput) Decision`

- [ ] **Step 1: Write the failing tests**

`internal/view/view_test.go`:
```go
package view

import "testing"

func TestSnapshotPane(t *testing.T) {
	s := Snapshot{Panes: []Pane{{PaneID: "a"}, {PaneID: "b", Agent: "codex"}}}
	if p, ok := s.Pane("b"); !ok || p.Agent != "codex" {
		t.Fatal("lookup")
	}
	if _, ok := s.Pane("z"); ok {
		t.Fatal("missing pane")
	}
}
```

`internal/engine/filter_test.go`:
```go
package engine

import (
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

func baseInput() FilterInput {
	d := dots.New("Security")
	d.Purpose = "p"
	return FilterInput{
		Dot:        d,
		Att:        state.DotAttachment{Slug: "security"},
		NewTurns:   []transcript.Turn{{Index: 1, User: "hi", Tools: []transcript.ToolUse{{Name: "Edit", Paths: []string{"/repo/web/app.tsx"}}}}},
		PaneBudget: 120,
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*FilterInput)
		want Reason
	}{
		{"runs by default", func(*FilterInput) {}, ReasonRun},
		{"paused all", func(in *FilterInput) { in.PausedAll = true }, ReasonPausedAll},
		{"paused", func(in *FilterInput) { in.Att.Paused = true }, ReasonPaused},
		{"observe never runs on turns", func(in *FilterInput) { in.Dot.Delivery.Mode = dots.ModeObserve }, ReasonObserve},
		{"on_turn off", func(in *FilterInput) { in.Dot.Trigger.OnTurn = false }, ReasonOff},
		{"only own dot turns", func(in *FilterInput) { in.NewTurns[0].FromDots = []string{"Security"} }, ReasonFromDot},
		{"other dot's turn still counts", func(in *FilterInput) { in.NewTurns[0].FromDots = []string{"Frontend"} }, ReasonRun},
		{"cooldown", func(in *FilterInput) { in.Att.CooldownLeft = 1 }, ReasonCooldown},
		{"glob miss", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"**/*.go"} }, ReasonGlobs},
		{"glob hit absolute path", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"**/*.tsx"} }, ReasonRun},
		{"glob hit base name", func(in *FilterInput) { in.Dot.Trigger.FileGlobs = []string{"*.tsx"} }, ReasonRun},
		{"dot budget", func(in *FilterInput) { in.Dot.Budget.MaxCallsPerSession = 2; in.DotCalls = 2 }, ReasonBudget},
		{"session budget", func(in *FilterInput) { in.PaneCalls = 120 }, ReasonSessionBudget},
	}
	for _, c := range cases {
		in := baseInput()
		c.mod(&in)
		got := Decide(in)
		if got.Reason != c.want || got.Run != (c.want == ReasonRun) {
			t.Errorf("%s: got %+v, want %s", c.name, got.Reason, c.want)
		}
	}
}

func TestDecideDropsOwnTurnsFromContext(t *testing.T) {
	in := baseInput()
	in.NewTurns = append(in.NewTurns, transcript.Turn{Index: 2, FromDots: []string{"Security"}})
	got := Decide(in)
	if !got.Run || len(got.Turns) != 1 || got.Turns[0].Index != 1 {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/view/ ./internal/engine/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/view/view.go`**

```go
// Package view holds the snapshot the engine streams to the UIs.
package view

import (
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

const (
	StatusIdle     = "idle"
	StatusThinking = "thinking"
	StatusPassed   = "passed"
	StatusFlagged  = "flagged"
	StatusError    = "error"
	StatusBudget   = "budget"
	StatusPaused   = "paused"
)

type DotState struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Glyph  string `json:"glyph"`
	Color  string `json:"color"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
	Calls  int    `json:"calls"`
	Budget int    `json:"budget"`
}

type Pane struct {
	PaneID string            `json:"pane_id"`
	Agent  string            `json:"agent"`
	Source string            `json:"source"`
	Calls  int               `json:"calls"`
	Budget int               `json:"budget"`
	Dots   []DotState        `json:"dots"`
	Cards  []queue.Card      `json:"cards"`
	Feed   []state.FeedEntry `json:"feed"`
}

type LibraryDot struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Glyph   string `json:"glyph"`
	Color   string `json:"color"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Mode    string `json:"mode"`
}

type Invalid struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Error string `json:"error"`
}

type Snapshot struct {
	Panes         []Pane       `json:"panes"`
	Library       []LibraryDot `json:"library"`
	Invalid       []Invalid    `json:"invalid"`
	InjectEnabled bool         `json:"inject_enabled"`
	PausedAll     bool         `json:"paused_all"`
}

func (s Snapshot) Pane(id string) (Pane, bool) {
	for _, p := range s.Panes {
		if p.PaneID == id {
			return p, true
		}
	}
	return Pane{}, false
}
```

- [ ] **Step 4: Write `internal/engine/filter.go`**

```go
// Package engine runs dots: it watches main agent panes, decides which dots
// should look at each finished turn, runs them, and routes their verdicts.
package engine

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

type Reason string

const (
	ReasonRun           Reason = "run"
	ReasonPausedAll     Reason = "paused_all"
	ReasonPaused        Reason = "paused"
	ReasonObserve       Reason = "observe"
	ReasonOff           Reason = "on_turn_off"
	ReasonFromDot       Reason = "from_dot"
	ReasonCooldown      Reason = "cooldown"
	ReasonGlobs         Reason = "globs"
	ReasonBudget        Reason = "budget"
	ReasonSessionBudget Reason = "session_budget"
)

type FilterInput struct {
	Dot        dots.Dot
	Att        state.DotAttachment
	NewTurns   []transcript.Turn
	PausedAll  bool
	DotCalls   int
	PaneCalls  int
	PaneBudget int
}

type Decision struct {
	Run    bool
	Reason Reason
	Turns  []transcript.Turn // new turns minus the ones this dot started
}

// Decide applies the spec's trigger filter (§6.3) to one dot.
func Decide(in FilterInput) Decision {
	switch {
	case in.PausedAll:
		return Decision{Reason: ReasonPausedAll}
	case in.Att.Paused:
		return Decision{Reason: ReasonPaused}
	case in.Dot.Delivery.Mode == dots.ModeObserve:
		return Decision{Reason: ReasonObserve}
	case !in.Dot.Trigger.OnTurn:
		return Decision{Reason: ReasonOff}
	}
	var turns []transcript.Turn
	for _, t := range in.NewTurns {
		if !t.IsFromDot(in.Dot.Name) {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return Decision{Reason: ReasonFromDot}
	}
	if in.Att.CooldownLeft > 0 {
		return Decision{Reason: ReasonCooldown}
	}
	if len(in.Dot.Trigger.FileGlobs) > 0 && !globsMatch(in.Dot.Trigger.FileGlobs, turns) {
		return Decision{Reason: ReasonGlobs}
	}
	if in.DotCalls >= in.Dot.Budget.MaxCallsPerSession {
		return Decision{Reason: ReasonBudget}
	}
	if in.PaneCalls >= in.PaneBudget {
		return Decision{Reason: ReasonSessionBudget}
	}
	return Decision{Run: true, Reason: ReasonRun, Turns: turns}
}

// globsMatch tries each touched path as-is, without its leading slash, and by
// base name, so "**/*.tsx" and "*.tsx" both match "/repo/web/app.tsx".
func globsMatch(globs []string, turns []transcript.Turn) bool {
	for _, t := range turns {
		for _, p := range t.TouchedPaths() {
			for _, g := range globs {
				for _, cand := range []string{p, strings.TrimPrefix(p, "/"), filepath.Base(p)} {
					if ok, _ := doublestar.Match(g, cand); ok {
						return true
					}
				}
			}
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/view/ ./internal/engine/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/view internal/engine/filter.go internal/engine/filter_test.go
git commit -m "feat: snapshot view types and the per-dot trigger filter"
```

---

### Task 13: Delivery formatting and guards

**Files:**
- Create: `internal/delivery/delivery.go`
- Test: `internal/delivery/delivery_test.go`

**Interfaces:**
- Consumes: `herdr.PaneInfo`, `herdr.StatusIdle`/`StatusDone`, `queue.Card`, `transcript.DotPrefix`.
- Produces:
  - `type Target interface{ PaneGet(ctx context.Context, paneID string) (herdr.PaneInfo, error); AgentPrompt(ctx context.Context, target, text string) error }`
  - `const MaxInjectChars = 2000`
  - `type Gate int`; consts `Now`, `Busy`, `Focused`; `(Gate) String() string`
  - `CanDeliver(p herdr.PaneInfo, requireUnfocused bool) Gate`
  - `Format(cards []queue.Card) string`; `TruncateRunes(s string, n int) string`
  - `Send(ctx context.Context, t Target, paneID string, cards []queue.Card, inject bool) (Gate, error)`

- [ ] **Step 1: Write the failing tests**

`internal/delivery/delivery_test.go`:
```go
package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
)

type fakeTarget struct {
	pane    herdr.PaneInfo
	getErr  error
	sent    []string
	sendErr error
}

func (f *fakeTarget) PaneGet(context.Context, string) (herdr.PaneInfo, error) { return f.pane, f.getErr }
func (f *fakeTarget) AgentPrompt(_ context.Context, _ string, text string) error {
	f.sent = append(f.sent, text)
	return f.sendErr
}

func TestCanDeliver(t *testing.T) {
	idle := herdr.PaneInfo{AgentStatus: herdr.StatusIdle}
	if CanDeliver(idle, true) != Now || CanDeliver(herdr.PaneInfo{AgentStatus: herdr.StatusDone}, false) != Now {
		t.Fatal("idle/done should deliver")
	}
	for _, st := range []herdr.AgentStatus{herdr.StatusWorking, herdr.StatusBlocked, herdr.StatusUnknown} {
		if CanDeliver(herdr.PaneInfo{AgentStatus: st}, false) != Busy {
			t.Fatalf("%s must be Busy", st)
		}
	}
	focused := herdr.PaneInfo{AgentStatus: herdr.StatusIdle, Focused: true}
	if CanDeliver(focused, true) != Focused || CanDeliver(focused, false) != Now {
		t.Fatal("focus only matters for inject")
	}
}

func TestFormatGroupsByDotInOrder(t *testing.T) {
	out := Format([]queue.Card{
		{Dot: "security", DotName: "Security", Glyph: "◆", Body: "JWT in localStorage."},
		{Dot: "frontend", DotName: "Frontend", Glyph: "▲", Body: "Add a skeleton."},
		{Dot: "security", DotName: "Security", Glyph: "◆", Body: "Rate-limit /login."},
	})
	want := "[◆ Security dot] JWT in localStorage.\n\nRate-limit /login.\n\n[▲ Frontend dot] Add a skeleton."
	if out != want {
		t.Fatalf("Format =\n%s\nwant\n%s", out, want)
	}
}

// Review Focus 4: the inject cap must never split a multi-byte rune.
func TestTruncateRunesIsRuneSafe(t *testing.T) {
	s := strings.Repeat("◆✨", 1500)
	got := TruncateRunes(s, MaxInjectChars)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != MaxInjectChars || !strings.HasSuffix(got, "…") {
		t.Fatalf("bad truncation: valid=%v runes=%d", utf8.ValidString(got), utf8.RuneCountInString(got))
	}
	if TruncateRunes("short", 10) != "short" {
		t.Fatal("short strings unchanged")
	}
}

func TestSend(t *testing.T) {
	cards := []queue.Card{{DotName: "Security", Glyph: "◆", Body: strings.Repeat("x", 3000)}}
	ctx := context.Background()

	ft := &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusWorking}}
	if g, err := Send(ctx, ft, "p", cards, false); g != Busy || err != nil || len(ft.sent) != 0 {
		t.Fatal("busy pane must not receive input")
	}
	ft = &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusIdle, Focused: true}}
	if g, _ := Send(ctx, ft, "p", cards, true); g != Focused || len(ft.sent) != 0 {
		t.Fatal("inject must not type into the focused pane")
	}
	ft = &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusIdle}}
	if g, err := Send(ctx, ft, "p", cards, true); g != Now || err != nil || utf8.RuneCountInString(ft.sent[0]) != MaxInjectChars {
		t.Fatalf("inject should be capped, got %d runes", utf8.RuneCountInString(ft.sent[0]))
	}
	ft = &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusIdle}}
	if _, _ = Send(ctx, ft, "p", cards, false); utf8.RuneCountInString(ft.sent[0]) <= MaxInjectChars {
		t.Fatal("approved (non-inject) messages are not capped")
	}
	ft = &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusIdle}, sendErr: errors.New("nope")}
	if _, err := Send(ctx, ft, "p", cards, false); err == nil {
		t.Fatal("prompt errors must surface")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/delivery/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/delivery/delivery.go`**

```go
// Package delivery sends dot messages into a main agent, but only when that
// cannot collide with the agent's work or the developer's typing.
package delivery

import (
	"context"
	"strings"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/transcript"
)

const MaxInjectChars = 2000

type Target interface {
	PaneGet(ctx context.Context, paneID string) (herdr.PaneInfo, error)
	AgentPrompt(ctx context.Context, target, text string) error
}

type Gate int

const (
	Now     Gate = iota // deliver now
	Busy                // agent not idle: wait for the next idle
	Focused             // inject only: developer is in that pane
)

func (g Gate) String() string {
	switch g {
	case Now:
		return "now"
	case Busy:
		return "busy"
	}
	return "focused"
}

func CanDeliver(p herdr.PaneInfo, requireUnfocused bool) Gate {
	if p.AgentStatus != herdr.StatusIdle && p.AgentStatus != herdr.StatusDone {
		return Busy
	}
	if requireUnfocused && p.Focused {
		return Focused
	}
	return Now
}

// Format puts each dot's header once, in first-seen order, followed by all of
// that dot's messages.
func Format(cards []queue.Card) string {
	var order []string
	groups := map[string][]queue.Card{}
	for _, c := range cards {
		if _, ok := groups[c.Dot]; !ok {
			order = append(order, c.Dot)
		}
		groups[c.Dot] = append(groups[c.Dot], c)
	}
	var parts []string
	for _, slug := range order {
		g := groups[slug]
		bodies := make([]string, len(g))
		for i, c := range g {
			bodies[i] = strings.TrimSpace(c.Body)
		}
		parts = append(parts, transcript.DotPrefix(g[0].Glyph, g[0].DotName)+" "+strings.Join(bodies, "\n\n"))
	}
	return strings.Join(parts, "\n\n")
}

func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func Send(ctx context.Context, t Target, paneID string, cards []queue.Card, inject bool) (Gate, error) {
	if len(cards) == 0 {
		return Now, nil
	}
	p, err := t.PaneGet(ctx, paneID)
	if err != nil {
		return Busy, err
	}
	if g := CanDeliver(p, inject); g != Now {
		return g, nil
	}
	text := Format(cards)
	if inject {
		text = TruncateRunes(text, MaxInjectChars)
	}
	return Now, t.AgentPrompt(ctx, paneID, text)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/delivery/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/delivery
git commit -m "feat: delivery formatting with idle/focus guards and rune-safe inject cap"
```

---

### Task 14: Engine core — attach, turn detection, runs, routing and delivery

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/deliver.go`
- Test: `internal/engine/engine_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3–13.
- Produces:
  - `type Deps struct{ Herdr herdr.Client; Backend func(kind string) (backend.Backend, error); Reader func(a state.Attachment) transcript.Reader; LoadDots func() ([]dots.Dot, []dots.LoadError); SaveDot func(d dots.Dot) error; Config config.Config; Queue *queue.Store; Attachments *state.Attachments; Usage *state.Usage; Feed *state.Feed; Logf func(format string, args ...any); PollInterval time.Duration; MaxFailures int }`
  - `New(d Deps) *Engine`; `(*Engine) Start(ctx context.Context)`; `ReloadDots()`; `Attach(ctx context.Context, paneID string, slugs []string) error`; `Detach(paneID, slug string) error`; `SendCards(ids []string) error`; `Dismiss(id string) error`; `Edit(id, body string) error`; `Snapshot() view.Snapshot`; `Watch() (<-chan view.Snapshot, func())`
  - Internal (used by Task 16 in the same package): `runDot(paneID string, d dots.Dot, turns []transcript.Turn, o runOpts)`, `runOpts{forceQueue, summon bool; question string}`, `key{pane, slug string}`, `e.reader(att)`, `e.setStatus`, `e.notify`, `e.broadcast()`, `e.dots`, `e.status`, `e.warned`, `e.pausedAll`.

Concurrency rules (state them in the code comments too):
- `e.mu` guards the engine's maps. Lock order is always `e.mu` → store mutexes; stores never call back into the engine.
- `e.dmu` serializes every delivery so poll, idle handling and manual sends can't double-send a card.
- Backend runs execute outside every lock, limited by the `sem` channel (`MaxParallelRuns`).

- [ ] **Step 1: Write the failing tests**

`internal/engine/engine_test.go`:
```go
package engine

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

type fakeBackend struct {
	mu       sync.Mutex
	verdicts []backend.Verdict // consumed in order; the last one repeats
	err      error
	calls    []backend.Request
}

func (f *fakeBackend) Kind() string                  { return "claude-cli" }
func (f *fakeBackend) Check(context.Context) error   { return nil }
func (f *fakeBackend) n() int                        { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeBackend) last() backend.Request         { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[len(f.calls)-1] }
func (f *fakeBackend) set(vs ...backend.Verdict)     { f.mu.Lock(); f.verdicts = vs; f.mu.Unlock() }
func (f *fakeBackend) fail(err error)                { f.mu.Lock(); f.err = err; f.mu.Unlock() }
func (f *fakeBackend) Run(_ context.Context, r backend.Request) (backend.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	if f.err != nil {
		return backend.Result{}, f.err
	}
	v := backend.Verdict{Relevant: false, Severity: "info"}
	if len(f.verdicts) > 0 {
		v = f.verdicts[0]
		if len(f.verdicts) > 1 {
			f.verdicts = f.verdicts[1:]
		}
	}
	return backend.Result{Verdict: v, InputTokens: 10, OutputTokens: 5}, nil
}

type fakeReader struct {
	mu    sync.Mutex
	turns []transcript.Turn
}

func (r *fakeReader) Source() string { return transcript.SourceClaude }
func (r *fakeReader) Turns(context.Context) ([]transcript.Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]transcript.Turn(nil), r.turns...), nil
}
func (r *fakeReader) add(t transcript.Turn) {
	r.mu.Lock()
	t.Index = len(r.turns) + 1
	r.turns = append(r.turns, t)
	r.mu.Unlock()
}

type rig struct {
	mu      sync.Mutex // guards readers
	srv     *herdrtest.Server
	e       *Engine
	be      *fakeBackend
	readers map[string]*fakeReader // by session value
	q       *queue.Store
	atts    *state.Attachments
	saved   []dots.Dot
}

const pane = "w1:p1"

func dot(name string, mode dots.Mode) dots.Dot {
	d := dots.New(name)
	d.Purpose = "purpose of " + name
	d.Delivery.Mode = mode
	return d
}

func newRig(t *testing.T, ds ...dots.Dot) *rig {
	t.Helper()
	srv := herdrtest.New(t)
	srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "feed.jsonl"))
	r := &rig{srv: srv, be: &fakeBackend{}, readers: map[string]*fakeReader{"s1": {}}, q: q, atts: atts}
	cfg := config.Default()
	cfg.TurnDebounceMs = 0
	r.e = New(Deps{
		Herdr:   &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"},
		Backend: func(string) (backend.Backend, error) { return r.be, nil },
		Reader: func(a state.Attachment) transcript.Reader {
			r.mu.Lock()
			defer r.mu.Unlock()
			if rd, ok := r.readers[a.SessionValue]; ok {
				return rd
			}
			return &fakeReader{}
		},
		LoadDots:     func() ([]dots.Dot, []dots.LoadError) { return ds, nil },
		SaveDot:      func(d dots.Dot) error { r.saved = append(r.saved, d); return nil },
		Config:       cfg,
		Queue:        q,
		Attachments:  atts,
		Usage:        usage,
		Feed:         feed,
		PollInterval: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.e.Start(ctx)
	return r
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func (r *rig) attach(t *testing.T, slugs ...string) {
	t.Helper()
	if err := r.e.Attach(context.Background(), pane, slugs); err != nil {
		t.Fatal(err)
	}
	if !r.srv.WaitSubscribed(pane, time.Second) {
		t.Fatal("engine did not subscribe to the pane")
	}
}

func (r *rig) setReader(session string, rd *fakeReader) {
	r.mu.Lock()
	r.readers[session] = rd
	r.mu.Unlock()
}

func (r *rig) turnEnds(t *testing.T, tr transcript.Turn) {
	r.readers["s1"].add(tr)
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
}

func lastSeen(r *rig, slug string) int {
	a, _ := r.atts.Get(pane)
	if da := a.Find(slug); da != nil {
		return da.LastSeenTurn
	}
	return -1
}

var critical = backend.Verdict{Relevant: true, Severity: "critical", Message: "JWT in localStorage"}

func TestCatchUpCreatesQueueCardAndReportsPending(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a pending card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Body != "JWT in localStorage" || c.Mode != "queue" || c.Status != queue.StatusPending || c.Glyph != "●" {
		t.Fatalf("card %+v", c)
	}
	eventually(t, "pending reported to herdr", func() bool {
		calls := r.srv.Calls("pane.report_metadata")
		return len(calls) > 0 && calls[len(calls)-1].Params["display_agent"] == "claude ◆1"
	})
	if req := r.be.last(); req.Cwd != "/repo" || req.Turns[0].User != "add login" {
		t.Fatalf("request %+v", req)
	}
}

func TestTurnEndPassMakesNoCard(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "rename a var"})
	eventually(t, "dot ran", func() bool { return r.be.n() == 1 })
	eventually(t, "status passed", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Dots) == 1 && p.Dots[0].Status == view.StatusPassed
	})
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("a pass must not create a card")
	}
}

func TestObserveDotNeverRunsOnTurns(t *testing.T) {
	r := newRig(t, dot("Copy", dots.ModeObserve))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "copy")
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "copy") == 2 })
	if r.be.n() != 0 {
		t.Fatalf("observe dot ran %d times", r.be.n())
	}
}

func TestLoopGuardSkipsTurnStartedByThisDot(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "[● Security dot] use cookies", FromDots: []string{"Security"}})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 0 {
		t.Fatal("dot must not review a turn it started")
	}
}

func TestBudgetStopsDotAndWarnsOnce(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Budget.MaxCallsPerSession = 1
	r := newRig(t, d)
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "first run", func() bool { return r.be.n() == 1 })
	eventually(t, "first run finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Dots[0].Status == view.StatusPassed
	})
	r.turnEnds(t, transcript.Turn{User: "two"})
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "budget status", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Dots[0].Status == view.StatusBudget
	})
	eventually(t, "one budget toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 1 || len(r.srv.Calls("notification.show")) != 1 {
		t.Fatalf("calls=%d toasts=%d", r.be.n(), len(r.srv.Calls("notification.show")))
	}
}

func TestInjectDeliversWhenIdleAndUnfocused(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "prompt sent", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
	text := r.srv.Calls("agent.prompt")[0].Params["text"].(string)
	if !strings.HasPrefix(text, "[● Security dot] JWT in localStorage") {
		t.Fatalf("prompt %q", text)
	}
	eventually(t, "card sent", func() bool { return len(r.q.Pending(pane)) == 0 })
}

func TestInjectWaitsWhileFocusedThenFlushes(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject))
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.Focused = true
	r.srv.SetPane(p)
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "auto card", func() bool {
		cs := r.q.Pending(pane)
		return len(cs) == 1 && cs[0].Status == queue.StatusAuto
	})
	time.Sleep(60 * time.Millisecond)
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("must not type into the focused pane")
	}
	p.Focused = false
	r.srv.SetPane(p)
	eventually(t, "flushed after focus left", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
}

func TestInjectForcedToQueueForDotStartedTurns(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject), dot("Frontend", dots.ModeQueue))
	r.attach(t, "security", "frontend")
	r.be.set(critical)
	r.turnEnds(t, transcript.Turn{User: "[● Frontend dot] add skeleton", FromDots: []string{"Frontend"}})
	eventually(t, "security card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Mode != "queue" || c.Status != queue.StatusPending {
		t.Fatalf("inject from a dot-started turn must be queued, got %+v", c)
	}
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("no auto inject on dot-started turns")
	}
}

func TestSendWhileBusyWaitsForIdle(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	id := r.q.Pending(pane)[0].ID
	r.srv.Push(pane, herdr.StatusWorking)
	if err := r.e.SendCards([]string{id}); err != nil {
		t.Fatal(err)
	}
	c, _ := r.q.Get(id)
	if c.Status != queue.StatusAuto || !c.Approved || len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatalf("busy send should wait: %+v", c)
	}
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "delivered on idle", func() bool { c, _ := r.q.Get(id); return c.Status == queue.StatusSent })
}

func TestSendRejectsSuggestCards(t *testing.T) {
	r := newRig(t, dot("Frontend", dots.ModeSuggest))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(backend.Verdict{Relevant: true, Severity: "warn", Message: "skeleton"})
	r.attach(t, "frontend")
	eventually(t, "suggest card", func() bool { return len(r.q.Pending(pane)) == 1 })
	eventually(t, "warn toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	if err := r.e.SendCards([]string{r.q.Pending(pane)[0].ID}); err == nil {
		t.Fatal("suggest cards cannot be sent")
	}
}

func TestDeliveryFailureReturnsCardToPending(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.SetPromptError("internal")
	id := r.q.Pending(pane)[0].ID
	_ = r.e.SendCards([]string{id})
	c, _ := r.q.Get(id)
	if c.Status != queue.StatusPending || c.Error == "" {
		t.Fatalf("failed delivery: %+v", c)
	}
}

func TestClosedPaneOrphansCards(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "attachment dropped", func() bool { _, ok := r.atts.Get(pane); return !ok })
	vis := r.q.Visible(pane)
	if len(vis) != 1 || vis[0].Status != queue.StatusOrphaned {
		t.Fatalf("visible %+v", vis)
	}
	if p, ok := r.e.Snapshot().Pane(pane); !ok || len(p.Cards) != 1 {
		t.Fatal("orphaned cards stay in the snapshot")
	}
}

// Review Focus 5: /clear gives the pane a new session id; follow it.
func TestNewAgentSessionIsFollowed(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "old conversation"})
	r.attach(t, "security")
	eventually(t, "catch-up run", func() bool { return r.be.n() == 1 })
	s2 := &fakeReader{}
	s2.add(transcript.Turn{User: "fresh conversation"})
	r.setReader("s2", s2)
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "dot reviews the new session", func() bool {
		return r.be.n() >= 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "fresh conversation"
	})
	a, _ := r.atts.Get(pane)
	if a.SessionValue != "s2" {
		t.Fatalf("attachment still on %q", a.SessionValue)
	}
}

func TestAttachUnknownDotFails(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	if err := r.e.Attach(context.Background(), pane, []string{"nope"}); err == nil {
		t.Fatal("unknown dot must fail")
	}
	if err := r.e.Attach(context.Background(), "w9:p9", []string{"security"}); err == nil {
		t.Fatal("unknown pane must fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/`
Expected: FAIL — undefined `New`, `Deps`, ...

- [ ] **Step 3: Write `internal/engine/engine.go`**

```go
package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

type Deps struct {
	Herdr        herdr.Client
	Backend      func(kind string) (backend.Backend, error)
	Reader       func(a state.Attachment) transcript.Reader
	LoadDots     func() ([]dots.Dot, []dots.LoadError)
	SaveDot      func(d dots.Dot) error
	Config       config.Config
	Queue        *queue.Store
	Attachments  *state.Attachments
	Usage        *state.Usage
	Feed         *state.Feed
	Logf         func(format string, args ...any)
	PollInterval time.Duration // 0 → 3s
	MaxFailures  int           // 0 → 3
}

type key struct{ pane, slug string }

type runOpts struct {
	forceQueue bool   // the turn was started by a dot: never auto-inject
	summon     bool   // manual summon: always a card, bypasses running flags
	question   string // summon question
}

// Engine owns all live state. Lock order: e.mu → store mutexes (stores never
// call back). e.dmu serializes deliveries. Backend runs happen outside locks,
// bounded by sem.
type Engine struct {
	d   Deps
	ctx context.Context

	mu        sync.Mutex
	dots      map[string]dots.Dot
	invalid   []dots.LoadError
	readers   map[string]transcript.Reader
	subs      map[string]context.CancelFunc
	running   map[key]bool
	rerun     map[key]bool
	status    map[key]string
	warned    map[key]bool
	pausedAll bool
	watchers  map[int]chan view.Snapshot
	nextW     int

	dmu sync.Mutex
	sem chan struct{}
}

func New(d Deps) *Engine {
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	if d.PollInterval <= 0 {
		d.PollInterval = 3 * time.Second
	}
	if d.MaxFailures <= 0 {
		d.MaxFailures = 3
	}
	e := &Engine{
		d:        d,
		ctx:      context.Background(),
		dots:     map[string]dots.Dot{},
		readers:  map[string]transcript.Reader{},
		subs:     map[string]context.CancelFunc{},
		running:  map[key]bool{},
		rerun:    map[key]bool{},
		status:   map[key]string{},
		warned:   map[key]bool{},
		watchers: map[int]chan view.Snapshot{},
		sem:      make(chan struct{}, max(1, d.Config.MaxParallelRuns)),
	}
	e.ReloadDots()
	return e
}

func (e *Engine) ReloadDots() {
	list, invalid := e.d.LoadDots()
	m := make(map[string]dots.Dot, len(list))
	for _, d := range list {
		m[d.Slug] = d
	}
	e.mu.Lock()
	e.dots, e.invalid = m, invalid
	e.mu.Unlock()
	e.broadcast()
}

// Start restores persisted attachments (dropping panes that no longer exist)
// and begins polling. Call once, before serving commands.
func (e *Engine) Start(ctx context.Context) {
	e.ctx = ctx
	for _, a := range e.d.Attachments.List() {
		if _, err := e.d.Herdr.PaneGet(ctx, a.PaneID); err != nil {
			e.dropPane(a.PaneID)
			continue
		}
		e.subscribe(a.PaneID)
	}
	go e.pollLoop()
}

func (e *Engine) Attach(ctx context.Context, paneID string, slugs []string) error {
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", paneID, err)
	}
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		att = state.Attachment{PaneID: paneID, Agent: p.Agent, Cwd: p.WorkDir()}
		if p.AgentSession != nil {
			att.SessionKind, att.SessionValue = p.AgentSession.Kind, p.AgentSession.Value
		}
	}
	r := e.reader(att)
	att.Source = r.Source()
	turns, err := r.Turns(ctx)
	if err != nil {
		e.d.Logf("pane %s: initial transcript read: %v", paneID, err)
	}
	e.mu.Lock()
	for _, slug := range slugs {
		d, ok := e.dots[slug]
		if !ok {
			e.mu.Unlock()
			return fmt.Errorf("unknown dot %q", slug)
		}
		if att.Find(slug) != nil {
			continue
		}
		// Catch-up: the newest Context.Turns turns count as "new" for this dot.
		att.Dots = append(att.Dots, state.DotAttachment{Slug: slug, LastSeenTurn: max(0, len(turns)-d.Context.Turns)})
		delete(e.warned, key{paneID, slug})
	}
	e.mu.Unlock()
	if err := e.d.Attachments.Put(att); err != nil {
		return err
	}
	e.subscribe(paneID)
	go e.processPane(paneID)
	e.broadcast()
	return nil
}

func (e *Engine) Detach(paneID, slug string) error {
	empty := false
	err := e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		kept := a.Dots[:0]
		for _, da := range a.Dots {
			if da.Slug != slug {
				kept = append(kept, da)
			}
		}
		a.Dots = kept
		empty = len(kept) == 0
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.status, key{paneID, slug})
	delete(e.warned, key{paneID, slug})
	e.mu.Unlock()
	if empty {
		e.unwatch(paneID)
		_ = e.d.Attachments.Delete(paneID)
	}
	e.broadcast()
	return nil
}

func (e *Engine) reader(att state.Attachment) transcript.Reader {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.readers[att.PaneID]; ok {
		return r
	}
	r := e.d.Reader(att)
	e.readers[att.PaneID] = r
	return r
}

func (e *Engine) dropReader(paneID string) {
	e.mu.Lock()
	delete(e.readers, paneID)
	e.mu.Unlock()
}

func (e *Engine) subscribe(paneID string) {
	e.mu.Lock()
	if _, ok := e.subs[paneID]; ok {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.subs[paneID] = cancel
	e.mu.Unlock()
	go e.watchPane(ctx, paneID)
}

func (e *Engine) unwatch(paneID string) {
	e.mu.Lock()
	if cancel, ok := e.subs[paneID]; ok {
		cancel()
		delete(e.subs, paneID)
	}
	delete(e.readers, paneID)
	e.mu.Unlock()
}

// watchPane turns working→idle transitions into (debounced) turn processing,
// resubscribing if the stream drops.
func (e *Engine) watchPane(ctx context.Context, paneID string) {
	for ctx.Err() == nil {
		ch, err := e.d.Herdr.Subscribe(ctx, paneID)
		if err != nil {
			e.d.Logf("pane %s: subscribe: %v", paneID, err)
			sleep(ctx, 2*time.Second)
			continue
		}
		var timer *time.Timer
		for ev := range ch {
			if timer != nil {
				timer.Stop()
			}
			if ev.Status == herdr.StatusIdle || ev.Status == herdr.StatusDone {
				timer = time.AfterFunc(e.d.Config.Debounce(), func() { e.onIdle(paneID) })
			}
		}
		if timer != nil {
			timer.Stop()
		}
		sleep(ctx, time.Second)
	}
}

func (e *Engine) onIdle(paneID string) {
	p, err := e.d.Herdr.PaneGet(e.ctx, paneID)
	if err != nil || (p.AgentStatus != herdr.StatusIdle && p.AgentStatus != herdr.StatusDone) {
		return
	}
	if a, ok := e.d.Attachments.Get(paneID); ok {
		e.checkSession(a, p)
	}
	e.processPane(paneID)
	e.flushAuto(paneID)
}

type job struct {
	dot        dots.Dot
	turns      []transcript.Turn
	forceQueue bool
}

// processPane reads the transcript and starts a run for every dot whose
// filter passes on the turns it hasn't seen.
func (e *Engine) processPane(paneID string) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return
	}
	turns, err := e.reader(att).Turns(e.ctx)
	if err != nil && !att.ForceScrollback {
		e.d.Logf("pane %s: transcript read failed (%v); falling back to scrollback", paneID, err)
		_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
			a.ForceScrollback, a.Source = true, transcript.SourceScrollback
		})
		e.dropReader(paneID)
		att, _ = e.d.Attachments.Get(paneID)
		turns, err = e.reader(att).Turns(e.ctx)
	}
	if err != nil {
		e.d.Logf("pane %s: read: %v", paneID, err)
		return
	}

	var jobs []job
	var budgetHit []dots.Dot
	e.mu.Lock()
	paneCalls := e.d.Usage.PaneCalls(paneID)
	_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		for i := range a.Dots {
			da := &a.Dots[i]
			d, ok := e.dots[da.Slug]
			if !ok {
				continue
			}
			start := min(da.LastSeenTurn, len(turns))
			newTurns := turns[start:]
			if len(newTurns) == 0 {
				da.LastSeenTurn = len(turns)
				continue
			}
			k := key{paneID, da.Slug}
			if e.running[k] {
				e.rerun[k] = true // pick these turns up when the current run ends
				continue
			}
			dec := Decide(FilterInput{
				Dot: d, Att: *da, NewTurns: newTurns, PausedAll: e.pausedAll,
				DotCalls: e.d.Usage.Calls(paneID, da.Slug), PaneCalls: paneCalls,
				PaneBudget: e.d.Config.SessionBudgetCalls,
			})
			da.LastSeenTurn = len(turns)
			switch dec.Reason {
			case ReasonCooldown:
				da.CooldownLeft--
			case ReasonBudget, ReasonSessionBudget:
				e.status[k] = view.StatusBudget
				if !e.warned[k] {
					e.warned[k] = true
					budgetHit = append(budgetHit, d)
				}
			}
			if !dec.Run {
				continue
			}
			e.running[k] = true
			paneCalls++
			jobs = append(jobs, job{dot: d, turns: tail(turns, d.Context.Turns), forceQueue: anyFromDot(newTurns)})
		}
	})
	e.mu.Unlock()

	for _, d := range budgetHit {
		e.notify(d, "budget reached; skipping turns until the budget resets")
	}
	for _, j := range jobs {
		go e.runDot(paneID, j.dot, j.turns, runOpts{forceQueue: j.forceQueue})
	}
	e.broadcast()
}

func (e *Engine) runDot(paneID string, d dots.Dot, turns []transcript.Turn, o runOpts) {
	k := key{paneID, d.Slug}
	defer func() {
		if !o.summon {
			e.finishRun(k)
		} else {
			e.broadcast()
		}
	}()
	select {
	case e.sem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	defer func() { <-e.sem }()

	e.setStatus(k, view.StatusThinking)
	att, _ := e.d.Attachments.Get(paneID)
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		e.runFailed(k, d, turn, err)
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.d.Config.RunTimeout())
	defer cancel()
	res, err := be.Run(ctx, backend.Request{
		DotName: d.Name, Purpose: d.Purpose, Verbosity: d.Context.Verbosity,
		Model: d.Backend.Model, Args: d.Backend.Args, RepoRead: d.Context.RepoRead,
		Cwd: att.Cwd, Turns: turns, Question: o.question, Force: o.summon,
	})
	_ = e.d.Usage.Record(paneID, d.Slug, res.InputTokens, res.OutputTokens)
	if err != nil {
		e.runFailed(k, d, turn, err)
		return
	}
	_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if da := a.Find(d.Slug); da != nil {
			da.Failures = 0
			if res.Verdict.Relevant {
				da.CooldownLeft = d.Trigger.Cooldown
			}
		}
	})
	if !res.Verdict.Relevant {
		e.feed(paneID, d.Slug, turn, state.OutcomePass, "")
		e.setStatus(k, view.StatusPassed)
		return
	}
	e.feed(paneID, d.Slug, turn, state.OutcomeFlagged, res.Verdict.Message)
	e.setStatus(k, view.StatusFlagged)
	e.route(paneID, d, turn, res.Verdict, o)
}

func (e *Engine) runFailed(k key, d dots.Dot, turn int, err error) {
	e.d.Logf("dot %s on %s: %v", d.Slug, k.pane, err)
	e.feed(k.pane, d.Slug, turn, state.OutcomeError, err.Error())
	e.setStatus(k, view.StatusError)
	stopped := false
	_ = e.d.Attachments.Update(k.pane, func(a *state.Attachment) {
		if da := a.Find(d.Slug); da != nil {
			da.Failures++
			if da.Failures >= e.d.MaxFailures && !da.Paused {
				da.Paused, stopped = true, true
			}
		}
	})
	if stopped {
		e.notify(d, fmt.Sprintf("stopped after %d failures: %s (press r in the Dots pane to reset)", e.d.MaxFailures, err))
	}
}

func (e *Engine) finishRun(k key) {
	e.mu.Lock()
	delete(e.running, k)
	again := e.rerun[k]
	delete(e.rerun, k)
	e.mu.Unlock()
	e.broadcast()
	if again {
		go e.processPane(k.pane)
	}
}

// route turns a relevant verdict into a card according to the dot's mode.
func (e *Engine) route(paneID string, d dots.Dot, turn int, v backend.Verdict, o runOpts) {
	mode := d.Delivery.Mode
	if o.summon || mode == dots.ModeObserve {
		mode = dots.ModeQueue
	}
	if mode == dots.ModeInject && (o.forceQueue || !e.d.Config.InjectEnabled) {
		mode = dots.ModeQueue
	}
	c := queue.Card{Dot: d.Slug, DotName: d.Name, Glyph: d.Glyph, PaneID: paneID, Turn: turn,
		Severity: v.Severity, Body: v.Message, Mode: string(mode)}
	if mode == dots.ModeInject {
		c.Status = queue.StatusAuto
	}
	if _, err := e.d.Queue.Add(c); err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
	if mode == dots.ModeSuggest && (v.Severity == "warn" || v.Severity == "critical") {
		e.notify(d, firstLine(v.Message))
	}
	e.reportPending(paneID)
	e.broadcast()
	if mode == dots.ModeInject {
		e.flushAuto(paneID)
	}
}

func (e *Engine) setStatus(k key, s string) {
	e.mu.Lock()
	e.status[k] = s
	e.mu.Unlock()
	e.broadcast()
}

func (e *Engine) notify(d dots.Dot, body string) {
	if err := e.d.Herdr.Notify(e.ctx, d.Glyph+" "+d.Name+" dot", body); err != nil {
		e.d.Logf("notify: %v", err)
	}
}

func (e *Engine) feed(paneID, slug string, turn int, outcome, detail string) {
	if err := e.d.Feed.Append(state.FeedEntry{Time: time.Now(), PaneID: paneID, Dot: slug, Turn: turn, Outcome: outcome, Detail: detail}); err != nil {
		e.d.Logf("feed: %v", err)
	}
}

func (e *Engine) Snapshot() view.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := view.Snapshot{InjectEnabled: e.d.Config.InjectEnabled, PausedAll: e.pausedAll}
	slugs := make([]string, 0, len(e.dots))
	for slug := range e.dots {
		slugs = append(slugs, slug)
	}
	sort.Slice(slugs, func(i, j int) bool { return strings.ToLower(e.dots[slugs[i]].Name) < strings.ToLower(e.dots[slugs[j]].Name) })
	for _, slug := range slugs {
		d := e.dots[slug]
		s.Library = append(s.Library, view.LibraryDot{Slug: slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
			Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode)})
	}
	for _, le := range e.invalid {
		s.Invalid = append(s.Invalid, view.Invalid{File: le.File, Line: le.Line, Error: le.Err.Error()})
	}
	seen := map[string]bool{}
	for _, a := range e.d.Attachments.List() {
		pv := view.Pane{PaneID: a.PaneID, Agent: a.Agent, Source: a.Source,
			Calls: e.d.Usage.PaneCalls(a.PaneID), Budget: e.d.Config.SessionBudgetCalls}
		for _, da := range a.Dots {
			d, ok := e.dots[da.Slug]
			if !ok {
				d = dots.Dot{Slug: da.Slug, Name: da.Slug, Glyph: "?"}
			}
			st := e.status[key{a.PaneID, da.Slug}]
			if st == "" {
				st = view.StatusIdle
			}
			if da.Paused && st != view.StatusError {
				st = view.StatusPaused
			}
			pv.Dots = append(pv.Dots, view.DotState{Slug: da.Slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
				Mode: string(d.Delivery.Mode), Status: st, Paused: da.Paused,
				Calls: e.d.Usage.Calls(a.PaneID, da.Slug), Budget: d.Budget.MaxCallsPerSession})
		}
		pv.Cards = e.d.Queue.Visible(a.PaneID)
		pv.Feed = e.d.Feed.Recent(a.PaneID, 20)
		s.Panes = append(s.Panes, pv)
		seen[a.PaneID] = true
	}
	for _, c := range e.d.Queue.All() {
		if seen[c.PaneID] || (c.Status != queue.StatusPending && c.Status != queue.StatusAuto && c.Status != queue.StatusOrphaned) {
			continue
		}
		seen[c.PaneID] = true
		s.Panes = append(s.Panes, view.Pane{PaneID: c.PaneID, Cards: e.d.Queue.Visible(c.PaneID), Feed: e.d.Feed.Recent(c.PaneID, 20)})
	}
	return s
}

// Watch returns a channel that always holds the latest snapshot (buffer 1).
func (e *Engine) Watch() (<-chan view.Snapshot, func()) {
	ch := make(chan view.Snapshot, 1)
	e.mu.Lock()
	id := e.nextW
	e.nextW++
	e.watchers[id] = ch
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.watchers, id)
		e.mu.Unlock()
	}
}

func (e *Engine) broadcast() {
	s := e.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ch := range e.watchers {
		select {
		case <-ch: // drop the stale snapshot
		default:
		}
		select {
		case ch <- s:
		default:
		}
	}
}

func tail(turns []transcript.Turn, n int) []transcript.Turn {
	if len(turns) > n {
		return turns[len(turns)-n:]
	}
	return turns
}

func anyFromDot(turns []transcript.Turn) bool {
	for _, t := range turns {
		if t.FromAnyDot() {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(line); len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return line
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
```

- [ ] **Step 4: Write `internal/engine/deliver.go`**

```go
package engine

import (
	"errors"
	"fmt"
	"time"

	"herdr-dots/internal/delivery"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// flushAuto delivers waiting cards: approved ones need an idle agent; inject
// ones also need the developer to be out of that pane.
func (e *Engine) flushAuto(paneID string) {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	var approved, inject []queue.Card
	for _, c := range e.d.Queue.Pending(paneID) {
		if c.Status != queue.StatusAuto {
			continue
		}
		if c.Approved {
			approved = append(approved, c)
		} else {
			inject = append(inject, c)
		}
	}
	if len(approved) > 0 {
		e.deliver(paneID, approved, false)
	}
	if len(inject) > 0 {
		e.deliver(paneID, inject, true)
	}
}

// deliver must be called with e.dmu held.
func (e *Engine) deliver(paneID string, cards []queue.Card, inject bool) {
	gate, err := delivery.Send(e.ctx, e.d.Herdr, paneID, cards, inject)
	for _, c := range cards {
		switch {
		case err != nil:
			_ = e.d.Queue.Fail(c.ID, err.Error())
		case gate == delivery.Now:
			_ = e.d.Queue.SetStatus(c.ID, queue.StatusSent)
		case c.Status == queue.StatusPending:
			_ = e.d.Queue.SetStatus(c.ID, queue.StatusAuto)
		}
	}
	if err != nil {
		e.d.Logf("deliver to %s: %v", paneID, err)
	}
	e.reportPending(paneID)
	e.broadcast()
}

// SendCards approves cards and delivers them now, or as soon as the agent is idle.
func (e *Engine) SendCards(ids []string) error {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	byPane := map[string][]queue.Card{}
	var order []string
	for _, id := range ids {
		c, ok := e.d.Queue.Get(id)
		if !ok {
			return fmt.Errorf("card %s: %w", id, queue.ErrNotFound)
		}
		if c.Mode == string(dots.ModeSuggest) {
			return fmt.Errorf("card %s is a suggestion; copy it instead of sending", id)
		}
		if err := e.d.Queue.Approve(id); err != nil {
			return err
		}
		c.Approved = true
		if _, ok := byPane[c.PaneID]; !ok {
			order = append(order, c.PaneID)
		}
		byPane[c.PaneID] = append(byPane[c.PaneID], c)
	}
	for _, p := range order {
		e.deliver(p, byPane[p], false)
	}
	return nil
}

func (e *Engine) Dismiss(id string) error {
	c, ok := e.d.Queue.Get(id)
	if !ok {
		return queue.ErrNotFound
	}
	if err := e.d.Queue.SetStatus(id, queue.StatusDismissed); err != nil {
		return err
	}
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

func (e *Engine) Edit(id, body string) error {
	if err := e.d.Queue.Edit(id, body); err != nil {
		return err
	}
	e.broadcast()
	return nil
}

func (e *Engine) pollLoop() {
	t := time.NewTicker(e.d.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.poll()
		}
	}
}

// poll detects closed panes and session changes (Herdr has no pane-closed
// event in its subscription stream) and flushes auto cards whose guard opened.
func (e *Engine) poll() {
	for _, a := range e.d.Attachments.List() {
		p, err := e.d.Herdr.PaneGet(e.ctx, a.PaneID)
		if herdr.IsNotFound(err) {
			e.dropPane(a.PaneID)
			continue
		}
		if err != nil {
			continue
		}
		e.checkSession(a, p)
		e.flushAuto(a.PaneID)
	}
}

func (e *Engine) dropPane(paneID string) {
	e.unwatch(paneID)
	_ = e.d.Attachments.Delete(paneID)
	_ = e.d.Usage.ResetPane(paneID)
	if n, err := e.d.Queue.OrphanPane(paneID); err != nil {
		e.d.Logf("orphan cards for %s: %v", paneID, err)
	} else if n > 0 {
		e.d.Logf("pane %s closed: %d card(s) orphaned", paneID, n)
	}
	e.broadcast()
}

// checkSession follows a new agent session in the same pane (/clear, restart).
func (e *Engine) checkSession(a state.Attachment, p herdr.PaneInfo) {
	if p.AgentSession == nil || p.AgentSession.Value == a.SessionValue {
		return
	}
	e.d.Logf("pane %s: agent session %q → %q", a.PaneID, a.SessionValue, p.AgentSession.Value)
	err := e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) {
		x.SessionKind, x.SessionValue = p.AgentSession.Kind, p.AgentSession.Value
		if p.Agent != "" {
			x.Agent = p.Agent
		}
		x.ForceScrollback = false
		for i := range x.Dots {
			x.Dots[i].LastSeenTurn = 0
		}
	})
	if errors.Is(err, state.ErrNoAttachment) {
		return
	}
	e.dropReader(a.PaneID)
	if att, ok := e.d.Attachments.Get(a.PaneID); ok {
		r := e.reader(att)
		_ = e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) { x.Source = r.Source() })
	}
}

func (e *Engine) reportPending(paneID string) {
	a, ok := e.d.Attachments.Get(paneID)
	agent := a.Agent
	if !ok || agent == "" {
		agent = "agent"
	}
	if err := e.d.Herdr.ReportPending(e.ctx, paneID, agent, len(e.d.Queue.Pending(paneID))); err != nil && !herdr.IsNotFound(err) {
		e.d.Logf("report pending: %v", err)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race -count=3 ./internal/engine/`
Expected: PASS on all three runs (`-count=3` shakes out timing flakes). If a test flakes, fix the race in the engine — do not lengthen sleeps in tests.

- [ ] **Step 6: Commit**

```bash
git add internal/engine
git commit -m "feat: engine core - attach, turn detection, dot runs, routing, guarded delivery"
```

---

### Task 15: Engine ↔ UI protocol (local IPC)

**Files:**
- Create: `internal/ipc/ipc.go`
- Test: `internal/ipc/ipc_test.go`

**Interfaces:**
- Consumes: `view.Snapshot`.
- Produces:
  - Command consts: `CmdPing`, `CmdWatch`, `CmdAttach`, `CmdDetach`, `CmdSend`, `CmdDismiss`, `CmdEdit`, `CmdSummon`, `CmdSetMode`, `CmdPause`, `CmdPauseAll`, `CmdReset`, `CmdReloadDots`
  - Arg types: `AttachArgs{PaneID string; Slugs []string}`, `DotArgs{PaneID, Slug string}` (detach, reset), `SendArgs{IDs []string}`, `CardArgs{ID, Body string}` (dismiss, edit), `SummonArgs{PaneID, Slug, Question string}`, `SetModeArgs{Slug, Mode string}`, `PauseArgs{PaneID, Slug string; Paused bool}`
  - `type Request struct{ ID int; Cmd string; Args json.RawMessage }`; `type Response struct{ ID int; OK bool; Error string; Snapshot *view.Snapshot }`
  - `type Handler interface{ Handle(ctx context.Context, cmd string, args json.RawMessage) error; Snapshot() view.Snapshot; Watch() (<-chan view.Snapshot, func()) }`
  - `Serve(ctx context.Context, ln net.Listener, h Handler) error`
  - `type Client struct{ Path string }`; `(Client) Call(ctx context.Context, cmd string, args any) error`; `(Client) Watch(ctx context.Context) (<-chan view.Snapshot, error)`

- [ ] **Step 1: Write the failing tests**

`internal/ipc/ipc_test.go`:
```go
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

type fakeHandler struct {
	mu      sync.Mutex
	got     []string
	args    []json.RawMessage
	updates chan view.Snapshot
	stopped bool
}

func (f *fakeHandler) Handle(_ context.Context, cmd string, args json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, cmd)
	f.args = append(f.args, args)
	if cmd == "explode" {
		return errors.New("boom")
	}
	return nil
}
func (f *fakeHandler) Snapshot() view.Snapshot { return view.Snapshot{PausedAll: true} }
func (f *fakeHandler) Watch() (<-chan view.Snapshot, func()) {
	return f.updates, func() { f.mu.Lock(); f.stopped = true; f.mu.Unlock() }
}

func serve(t *testing.T) (*fakeHandler, Client) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "ipc")
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHandler{updates: make(chan view.Snapshot, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go Serve(ctx, ln, h)
	return h, Client{Path: path}
}

func TestCallRoundTripAndErrors(t *testing.T) {
	h, c := serve(t)
	ctx := context.Background()
	if err := c.Call(ctx, CmdAttach, AttachArgs{PaneID: "w1:p1", Slugs: []string{"security"}}); err != nil {
		t.Fatal(err)
	}
	var a AttachArgs
	_ = json.Unmarshal(h.args[0], &a)
	if h.got[0] != CmdAttach || a.PaneID != "w1:p1" || a.Slugs[0] != "security" {
		t.Fatalf("handler got %v %+v", h.got, a)
	}
	if err := c.Call(ctx, "explode", nil); err == nil || err.Error() != "boom" {
		t.Fatalf("want boom, got %v", err)
	}
	if err := c.Call(ctx, CmdPing, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWatchStreamsInitialAndUpdates(t *testing.T) {
	h, c := serve(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := <-ch
	if !first.PausedAll {
		t.Fatal("first message is the current snapshot")
	}
	h.updates <- view.Snapshot{InjectEnabled: true}
	select {
	case s := <-ch:
		if !s.InjectEnabled {
			t.Fatal("update not delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("no update")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		stopped := h.stopped
		h.mu.Unlock()
		if stopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not stop watching after client left")
}

func TestCallFailsFastWithoutServer(t *testing.T) {
	c := Client{Path: filepath.Join(t.TempDir(), "none.sock")}
	if err := c.Call(context.Background(), CmdPing, nil); err == nil {
		t.Fatal("want dial error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ipc/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/ipc/ipc.go`**

```go
// Package ipc is the newline-delimited JSON protocol between the engine and
// the Bubble Tea UIs over the engine's local unix socket.
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"herdr-dots/internal/view"
)

const (
	CmdPing       = "ping"
	CmdWatch      = "watch"
	CmdAttach     = "attach"
	CmdDetach     = "detach"
	CmdSend       = "send"
	CmdDismiss    = "dismiss"
	CmdEdit       = "edit"
	CmdSummon     = "summon"
	CmdSetMode    = "set_mode"
	CmdPause      = "pause"
	CmdPauseAll   = "pause_all"
	CmdReset      = "reset"
	CmdReloadDots = "reload_dots"
)

type AttachArgs struct {
	PaneID string   `json:"pane_id"`
	Slugs  []string `json:"slugs"`
}

type DotArgs struct {
	PaneID string `json:"pane_id"`
	Slug   string `json:"slug"`
}

type SendArgs struct {
	IDs []string `json:"ids"`
}

type CardArgs struct {
	ID   string `json:"id"`
	Body string `json:"body,omitempty"`
}

type SummonArgs struct {
	PaneID   string `json:"pane_id"`
	Slug     string `json:"slug"`
	Question string `json:"question,omitempty"`
}

type SetModeArgs struct {
	Slug string `json:"slug"`
	Mode string `json:"mode"`
}

type PauseArgs struct {
	PaneID string `json:"pane_id"`
	Slug   string `json:"slug"`
	Paused bool   `json:"paused"`
}

type Request struct {
	ID   int             `json:"id"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

type Response struct {
	ID       int            `json:"id"`
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Snapshot *view.Snapshot `json:"snapshot,omitempty"`
}

type Handler interface {
	Handle(ctx context.Context, cmd string, args json.RawMessage) error
	Snapshot() view.Snapshot
	Watch() (<-chan view.Snapshot, func())
}

func Serve(ctx context.Context, ln net.Listener, h Handler) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go serveConn(ctx, conn, h)
	}
}

func serveConn(ctx context.Context, conn net.Conn, h Handler) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.Cmd == CmdWatch {
			stream(ctx, conn, enc, req.ID, h)
			return
		}
		resp := Response{ID: req.ID, OK: true}
		if err := h.Handle(ctx, req.Cmd, req.Args); err != nil {
			resp.OK, resp.Error = false, err.Error()
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func stream(ctx context.Context, conn net.Conn, enc *json.Encoder, id int, h Handler) {
	ch, stop := h.Watch()
	defer stop()
	s := h.Snapshot()
	if enc.Encode(Response{ID: id, OK: true, Snapshot: &s}) != nil {
		return
	}
	gone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, conn); close(gone) }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			return
		case s := <-ch:
			if enc.Encode(Response{ID: id, OK: true, Snapshot: &s}) != nil {
				return
			}
		}
	}
}

type Client struct{ Path string }

func (c Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", c.Path)
}

func (c Client) Call(ctx context.Context, cmd string, args any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	var raw json.RawMessage
	if args != nil {
		if raw, err = json.Marshal(args); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: cmd, Args: raw}); err != nil {
		return err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

// Watch streams snapshots (latest wins) until ctx ends or the engine goes
// away; the channel is then closed.
func (c Client) Watch(ctx context.Context) (<-chan view.Snapshot, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: CmdWatch}); err != nil {
		conn.Close()
		return nil, err
	}
	ch := make(chan view.Snapshot, 1)
	go func() { <-ctx.Done(); conn.Close() }()
	go func() {
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var r Response
			if err := dec.Decode(&r); err != nil {
				return
			}
			if r.Snapshot == nil {
				continue
			}
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- *r.Snapshot:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/ipc/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ipc
git commit -m "feat: engine-UI IPC protocol with request/response and snapshot streaming"
```

---

### Task 16: Engine control commands and IPC handling

**Files:**
- Create: `internal/engine/control.go`, `internal/engine/handle.go`
- Test: `internal/engine/control_test.go`

**Interfaces:**
- Consumes: engine internals from Task 14 (`runDot`, `runOpts`, `key`, `reader`, `setStatus`, `broadcast`, `e.dots`, `e.warned`, `e.pausedAll`), ipc arg types (Task 15).
- Produces: `(*Engine) Summon(ctx context.Context, paneID, slug, question string) error`; `SetMode(slug string, mode dots.Mode) error`; `SetPaused(paneID, slug string, paused bool) error`; `TogglePauseAll()`; `Reset(paneID, slug string) error`; `Handle(ctx context.Context, cmd string, raw json.RawMessage) error` — so `*Engine` satisfies `ipc.Handler`.

- [ ] **Step 1: Write the failing tests**

`internal/engine/control_test.go`:
```go
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

var _ ipc.Handler = (*Engine)(nil)

func TestSummonObserveDotAlwaysQueuesACard(t *testing.T) {
	r := newRig(t, dot("Copy", dots.ModeObserve))
	r.readers["s1"].add(transcript.Turn{User: "write hero copy"})
	r.attach(t, "copy")
	r.be.set(backend.Verdict{Relevant: true, Severity: "info", Message: "Lead with the outcome."})
	if err := r.e.Summon(context.Background(), pane, "copy", "is the headline good?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "card from summon", func() bool { return len(r.q.Pending(pane)) == 1 })
	req := r.be.last()
	if !req.Force || req.Question != "is the headline good?" || req.Turns[0].User != "write hero copy" {
		t.Fatalf("request %+v", req)
	}
	if c := r.q.Pending(pane)[0]; c.Mode != "queue" {
		t.Fatalf("summon cards are queue cards, got %s", c.Mode)
	}
	if err := r.e.Summon(context.Background(), pane, "nope", ""); err == nil {
		t.Fatal("unknown dot")
	}
	if err := r.e.Summon(context.Background(), "w9:p9", "copy", ""); err == nil {
		t.Fatal("unattached pane")
	}
}

func TestThreeFailuresStopDotUntilReset(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.be.fail(errors.New("claude is not logged in: run `claude` and type /login"))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "run 1", func() bool { return r.be.n() == 1 })
	eventually(t, "run 1 done", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Dots[0].Status == view.StatusError })
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "run 2", func() bool { return r.be.n() == 2 })
	eventually(t, "run 2 done", func() bool { a, _ := r.atts.Get(pane); return a.Find("security").Failures == 2 })
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "stopped", func() bool { a, _ := r.atts.Get(pane); return a.Find("security").Paused })
	eventually(t, "stop toast", func() bool {
		for _, c := range r.srv.Calls("notification.show") {
			if strings.Contains(c.Params["body"].(string), "stopped after 3 failures") {
				return true
			}
		}
		return false
	})
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusError {
		t.Fatalf("a stopped dot shows error, got %s", p.Dots[0].Status)
	}
	if err := r.e.Reset(pane, "security"); err != nil {
		t.Fatal(err)
	}
	a, _ := r.atts.Get(pane)
	if da := a.Find("security"); da.Paused || da.Failures != 0 {
		t.Fatalf("reset %+v", da)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusIdle {
		t.Fatalf("status after reset %s", p.Dots[0].Status)
	}
}

func TestPauseAllStopsRuns(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.e.TogglePauseAll()
	if !r.e.Snapshot().PausedAll {
		t.Fatal("paused all")
	}
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	if r.be.n() != 0 {
		t.Fatal("no runs while paused")
	}
	r.e.TogglePauseAll()
	if r.e.Snapshot().PausedAll {
		t.Fatal("resumed")
	}
}

func TestSetPausedAndSetMode(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	if err := r.e.SetPaused(pane, "security", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Dots[0].Status != view.StatusPaused {
		t.Fatalf("status %s", p.Dots[0].Status)
	}
	if err := r.e.SetPaused(pane, "nope", true); err == nil {
		t.Fatal("unknown dot")
	}
	if err := r.e.SetMode("security", dots.ModeSuggest); err != nil {
		t.Fatal(err)
	}
	if len(r.saved) != 1 || r.saved[0].Delivery.Mode != dots.ModeSuggest {
		t.Fatalf("saved %+v", r.saved)
	}
	if err := r.e.SetMode("security", "shout"); err == nil {
		t.Fatal("invalid mode")
	}
}

func TestHandleRoutesCommands(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	ctx := context.Background()
	raw, _ := json.Marshal(ipc.AttachArgs{PaneID: pane, Slugs: []string{"security"}})
	if err := r.e.Handle(ctx, ipc.CmdAttach, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.atts.Get(pane); !ok {
		t.Fatal("attach via Handle")
	}
	raw, _ = json.Marshal(ipc.DotArgs{PaneID: pane, Slug: "security"})
	if err := r.e.Handle(ctx, ipc.CmdDetach, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.atts.Get(pane); ok {
		t.Fatal("detaching the last dot removes the attachment")
	}
	if err := r.e.Handle(ctx, ipc.CmdAttach, nil); err == nil {
		t.Fatal("missing args must error")
	}
	if err := r.e.Handle(ctx, "bogus", nil); err == nil {
		t.Fatal("unknown command must error")
	}
	if err := r.e.Handle(ctx, ipc.CmdPing, nil); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/`
Expected: FAIL — undefined `Summon`, `Reset`, `TogglePauseAll`, `SetPaused`, `SetMode`, `Handle`.

- [ ] **Step 3: Write `internal/engine/control.go`**

```go
package engine

import (
	"context"
	"fmt"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

// Summon runs one dot now on recent turns. It ignores mode, filter and budget
// (usage is still counted) and always produces a card.
func (e *Engine) Summon(ctx context.Context, paneID, slug, question string) error {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if att.Find(slug) == nil {
		return fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	e.mu.Lock()
	d, ok := e.dots[slug]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	turns, err := e.reader(att).Turns(ctx)
	if err != nil {
		return fmt.Errorf("read conversation: %w", err)
	}
	go e.runDot(paneID, d, tail(turns, d.Context.Turns), runOpts{summon: true, question: question})
	return nil
}

func (e *Engine) SetMode(slug string, mode dots.Mode) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid mode %q", mode)
	}
	e.mu.Lock()
	d, ok := e.dots[slug]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	d.Delivery.Mode = mode
	if err := e.d.SaveDot(d); err != nil {
		return err
	}
	e.ReloadDots()
	return nil
}

func (e *Engine) SetPaused(paneID, slug string, paused bool) error {
	found := false
	err := e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if da := a.Find(slug); da != nil {
			found = true
			da.Paused = paused
			if !paused {
				da.Failures = 0
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	if !paused {
		e.setStatus(key{paneID, slug}, view.StatusIdle)
		return nil
	}
	e.mu.Lock()
	delete(e.status, key{paneID, slug})
	e.mu.Unlock()
	e.broadcast()
	return nil
}

func (e *Engine) TogglePauseAll() {
	e.mu.Lock()
	e.pausedAll = !e.pausedAll
	e.mu.Unlock()
	e.broadcast()
}

// Reset clears a dot's failure stop and budget warning.
func (e *Engine) Reset(paneID, slug string) error {
	if err := e.SetPaused(paneID, slug, false); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.warned, key{paneID, slug})
	e.mu.Unlock()
	return nil
}
```

- [ ] **Step 4: Write `internal/engine/handle.go`**

```go
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
)

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return errors.New("missing arguments")
	}
	return json.Unmarshal(raw, v)
}

// Handle implements ipc.Handler.
func (e *Engine) Handle(ctx context.Context, cmd string, raw json.RawMessage) error {
	switch cmd {
	case ipc.CmdPing:
		return nil
	case ipc.CmdReloadDots:
		e.ReloadDots()
		return nil
	case ipc.CmdPauseAll:
		e.TogglePauseAll()
		return nil
	case ipc.CmdAttach:
		var a ipc.AttachArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Attach(ctx, a.PaneID, a.Slugs)
	case ipc.CmdDetach:
		var a ipc.DotArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Detach(a.PaneID, a.Slug)
	case ipc.CmdReset:
		var a ipc.DotArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Reset(a.PaneID, a.Slug)
	case ipc.CmdSend:
		var a ipc.SendArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SendCards(a.IDs)
	case ipc.CmdDismiss:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Dismiss(a.ID)
	case ipc.CmdEdit:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Edit(a.ID, a.Body)
	case ipc.CmdSummon:
		var a ipc.SummonArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Summon(ctx, a.PaneID, a.Slug, a.Question)
	case ipc.CmdSetMode:
		var a ipc.SetModeArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SetMode(a.Slug, dots.Mode(a.Mode))
	case ipc.CmdPause:
		var a ipc.PauseArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.SetPaused(a.PaneID, a.Slug, a.Paused)
	}
	return fmt.Errorf("unknown command %q", cmd)
}
```

Note: `Summon` launches the run with the engine's own context (`e.ctx`), not the IPC request's `ctx`, so the run survives the UI disconnecting.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race -count=3 ./internal/engine/`
Expected: PASS on all three runs.

- [ ] **Step 6: Commit**

```bash
git add internal/engine
git commit -m "feat: engine control commands (summon, mode, pause, reset) and IPC handling"
```

---

### Task 17: `engine` subcommand — single-instance lock, detached start, wiring

**Files:**
- Create: `internal/lockfile/lockfile.go`, `cmd/herdr-dots/engine.go`
- Modify: `cmd/herdr-dots/main.go` (replace `notYet("engine")` with `runEngine`)
- Test: `internal/lockfile/lockfile_test.go`, `cmd/herdr-dots/engine_test.go`

**Interfaces:**
- Consumes: paths, config, herdr, queue, state, dots, backend, transcript, engine, ipc.
- Produces:
  - `lockfile.TryLock(path string) (*Lock, error)`; `(*Lock) Release() error`; `lockfile.Held(path string) bool`; `var lockfile.ErrLocked`
  - `ensureEngine(p paths.Paths) error` — used by every UI and action subcommand
  - `buildEngine(p paths.Paths, hc herdr.Client, cfg config.Config, logf func(string, ...any)) (*engine.Engine, error)`
  - `waitForSocket(path string, d time.Duration) error`

- [ ] **Step 1: Write the failing tests**

`internal/lockfile/lockfile_test.go`:
```go
package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSingleHolder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.lock")
	l, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Held(p) {
		t.Fatal("Held should see the lock")
	}
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if Held(p) {
		t.Fatal("released lock must not be held")
	}
	l2, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
}
```

`cmd/herdr-dots/engine_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/paths"
)

func TestBuildEngineLoadsDotsFromConfigDir(t *testing.T) {
	root := t.TempDir()
	p := paths.New(filepath.Join(root, "c"), filepath.Join(root, "s"), "/fake.sock")
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	d, _ := dots.FromTemplate("security")
	if err := dots.Save(p.DotsDir(), d, true); err != nil {
		t.Fatal(err)
	}
	srv := herdrtest.New(t)
	e, err := buildEngine(p, &herdr.SocketClient{Path: srv.Path}, config.Default(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if len(s.Library) != 1 || s.Library[0].Slug != "security" {
		t.Fatalf("library %+v", s.Library)
	}
	if err := e.SetMode("security", dots.ModeSuggest); err != nil {
		t.Fatal(err)
	}
	got, _ := dots.Load(p.DotsDir(), "security")
	if got.Delivery.Mode != dots.ModeSuggest {
		t.Fatal("SetMode must persist to the dot file")
	}
}

func TestWaitForSocketTimesOut(t *testing.T) {
	start := time.Now()
	err := waitForSocket(filepath.Join(t.TempDir(), "none.sock"), 200*time.Millisecond)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestRotateLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.log")
	os.WriteFile(p, make([]byte, maxLogBytes+1), 0o600)
	rotateLog(p)
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("big log should rotate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lockfile/ ./cmd/herdr-dots/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/lockfile/lockfile.go`**

```go
// Package lockfile guarantees one engine per Herdr session with flock(2). The
// kernel releases the lock if the engine dies, so stale locks can't happen.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrLocked = errors.New("lock held by another process")

type Lock struct{ f *os.File }

func TryLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f: f}, nil
}

func (l *Lock) Release() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// Held reports whether another holder currently owns the lock.
func Held(path string) bool {
	l, err := TryLock(path)
	if errors.Is(err, ErrLocked) {
		return true
	}
	if err == nil {
		_ = l.Release()
	}
	return false
}
```

- [ ] **Step 4: Write `cmd/herdr-dots/engine.go`**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/engine"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

const maxLogBytes = 5 << 20

func runEngine(args []string) error {
	fs := flag.NewFlagSet("engine", flag.ContinueOnError)
	ensure := fs.Bool("ensure", false, "start the engine in the background if it isn't running, then exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return err
	}
	if *ensure {
		return ensureEngine(p)
	}
	return serveEngine(p)
}

// ensureEngine starts a detached engine for this Herdr session unless one
// already holds the lock, then waits until its socket accepts connections.
func ensureEngine(p paths.Paths) error {
	if lockfile.Held(p.EngineLock()) {
		return waitForSocket(p.EngineSock(), 3*time.Second)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(p.EngineLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "engine")
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive the hook/pane that started us
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	_ = cmd.Process.Release()
	return waitForSocket(p.EngineSock(), 5*time.Second)
}

func waitForSocket(path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("engine socket %s not ready: %w", path, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func rotateLog(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
}

func buildEngine(p paths.Paths, hc herdr.Client, cfg config.Config, logf func(string, ...any)) (*engine.Engine, error) {
	q, err := queue.Open(p.QueueFile())
	if err != nil {
		return nil, err
	}
	atts, err := state.OpenAttachments(p.AttachmentsFile())
	if err != nil {
		return nil, err
	}
	usage, err := state.OpenUsage(p.UsageFile())
	if err != nil {
		return nil, err
	}
	feed, err := state.OpenFeed(p.FeedFile())
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	return engine.New(engine.Deps{
		Herdr:   hc,
		Backend: func(kind string) (backend.Backend, error) { return backend.New(kind, backend.ExecRunner{}) },
		Reader: func(a state.Attachment) transcript.Reader {
			return transcript.Select(transcript.SelectOptions{
				Agent: a.Agent, SessionKind: a.SessionKind, SessionValue: a.SessionValue,
				ForceScrollback: a.ForceScrollback, Home: home, Pane: hc, PaneID: a.PaneID,
			})
		},
		LoadDots:    func() ([]dots.Dot, []dots.LoadError) { return dots.LoadAll(p.DotsDir()) },
		SaveDot:     func(d dots.Dot) error { return dots.Save(p.DotsDir(), d, false) },
		Config:      cfg,
		Queue:       q,
		Attachments: atts,
		Usage:       usage,
		Feed:        feed,
		Logf:        logf,
	}), nil
}

func serveEngine(p paths.Paths) error {
	lock, err := lockfile.TryLock(p.EngineLock())
	if errors.Is(err, lockfile.ErrLocked) {
		return nil // another engine owns this session
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	rotateLog(p.EngineLog())
	logger := log.New(os.Stderr, "herdr-dots ", log.LstdFlags)

	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		logger.Printf("config: %v (using defaults)", err)
	}
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return err
	}
	e, err := buildEngine(p, hc, cfg, logger.Printf)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	sock := p.EngineSock()
	_ = os.Remove(sock) // stale socket from a dead engine; we hold the lock
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	_ = os.Chmod(sock, 0o600)
	defer os.Remove(sock)

	e.Start(ctx)
	go watchHerdr(ctx, cancel, hc, logger)
	logger.Printf("engine started: session dir %s, socket %s", p.SessionDir, sock)
	err = ipc.Serve(ctx, ln, e)
	logger.Printf("engine stopped")
	return err
}

// watchHerdr exits the engine once Herdr has been unreachable for 30s.
func watchHerdr(ctx context.Context, cancel context.CancelFunc, hc herdr.Client, logger *log.Logger) {
	var downSince time.Time
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, c := context.WithTimeout(ctx, 3*time.Second)
			err := hc.Ping(pctx)
			c()
			if err == nil {
				downSince = time.Time{}
				continue
			}
			if downSince.IsZero() {
				downSince = time.Now()
			}
			if time.Since(downSince) > 30*time.Second {
				logger.Printf("herdr unreachable for 30s (%v); exiting", err)
				cancel()
				return
			}
		}
	}
}
```

In `cmd/herdr-dots/main.go`, change `"engine": notYet("engine"),` to `"engine": runEngine,`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/lockfile/ ./cmd/herdr-dots/ && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/lockfile cmd/herdr-dots
git commit -m "feat: engine subcommand with flock single-instance, detached start and herdr liveness"
```

---

### Task 18: Dots side pane UI

**Files:**
- Create: `internal/ui/style/style.go`, `internal/ui/pane/model.go`, `cmd/herdr-dots/ui.go`
- Modify: `cmd/herdr-dots/main.go` (replace `notYet("pane")` with `runPane`)
- Test: `internal/ui/pane/model_test.go`

**Interfaces:**
- Consumes: `view.Snapshot`, ipc commands and arg types, `queue.Card`, `dots.Mode.Next`, `ensureEngine`.
- Produces:
  - `style.Color(name string) lipgloss.Color`; `style.Glyph(glyph, color string) string`; `style.Severity(sev string) lipgloss.Style`; styles `style.Title`, `style.Dim`, `style.Selected`, `style.Err`
  - `pane.Caller` interface `{ Call(ctx context.Context, cmd string, args any) error }` (satisfied by `ipc.Client`)
  - `pane.New(target string, c Caller) Model`; `pane.SnapshotMsg` (= `view.Snapshot`); `pane.DisconnectedMsg`
  - In `cmd`: `const envTarget = "HERDR_DOTS_TARGET"`; `resolveTarget() string`; `pumpSnapshots(ctx context.Context, p paths.Paths, cl ipc.Client, send func(tea.Msg), wrap func(view.Snapshot) tea.Msg, disconnected tea.Msg)`; `runPane(args []string) error`

- [ ] **Step 1: Write the failing tests**

`internal/ui/pane/model_test.go`:
```go
package pane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/view"
)

type call struct {
	cmd  string
	args string
}

type fakeCaller struct{ calls []call }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, call{cmd, string(b)})
	return nil
}

func snap() view.Snapshot {
	return view.Snapshot{Panes: []view.Pane{{
		PaneID: "w1:p1", Agent: "claude", Calls: 3, Budget: 120,
		Dots: []view.DotState{
			{Slug: "security", Name: "Security", Glyph: "◆", Color: "red", Mode: "queue", Status: "thinking"},
			{Slug: "frontend", Name: "Frontend", Glyph: "▲", Color: "cyan", Mode: "suggest", Status: "passed"},
		},
		Cards: []queue.Card{
			{ID: "1", Dot: "security", DotName: "Security", Glyph: "◆", Severity: "critical", Body: "JWT in localStorage", Mode: "queue", Status: queue.StatusPending, Turn: 4},
			{ID: "2", Dot: "frontend", DotName: "Frontend", Glyph: "▲", Severity: "info", Body: "Add a skeleton", Mode: "suggest", Status: queue.StatusPending, Turn: 4},
		},
	}}}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends a key and runs any returned command, like the Bubble Tea runtime.
func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				m, _ = m.Update(msg)
			}
		}
	}
	return m
}

func ready(fc *fakeCaller) tea.Model {
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(snap()))
	return m
}

func TestViewShowsRosterAndQueue(t *testing.T) {
	v := ready(&fakeCaller{}).View()
	for _, want := range []string{"claude", "w1:p1", "Security", "Frontend", "thinking", "QUEUE (2)", "JWT in localStorage", "CRITICAL", "budget 3/120"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
}

func TestEnterSendsSelectedCard(t *testing.T) {
	fc := &fakeCaller{}
	press(ready(fc), "enter")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdSend || fc.calls[0].args != `{"ids":["1"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSuggestCardCannotBeSent(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "j", "enter")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "copy") {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSendAllSkipsSuggestions(t *testing.T) {
	fc := &fakeCaller{}
	press(ready(fc), "a")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["1"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDismissEditSummonModePause(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "x")
	m = press(m, "e", "!", "ctrl+s")
	m = press(m, "tab") // focus roster (Security selected)
	m = press(m, "m")
	m = press(m, "p")
	m = press(m, "s", "w", "h", "y", "enter")
	want := []string{ipc.CmdDismiss, ipc.CmdEdit, ipc.CmdSetMode, ipc.CmdPause, ipc.CmdSummon}
	if len(fc.calls) != len(want) {
		t.Fatalf("calls %+v", fc.calls)
	}
	for i, c := range want {
		if fc.calls[i].cmd != c {
			t.Fatalf("call %d = %s, want %s (%+v)", i, fc.calls[i].cmd, c, fc.calls)
		}
	}
	if fc.calls[1].args != `{"id":"1","body":"JWT in localStorage!"}` {
		t.Errorf("edit args %s", fc.calls[1].args)
	}
	if fc.calls[2].args != `{"slug":"security","mode":"inject"}` {
		t.Errorf("mode args %s", fc.calls[2].args)
	}
	if fc.calls[3].args != `{"pane_id":"w1:p1","slug":"security","paused":true}` {
		t.Errorf("pause args %s", fc.calls[3].args)
	}
	if fc.calls[4].args != `{"pane_id":"w1:p1","slug":"security","question":"why"}` {
		t.Errorf("summon args %s", fc.calls[4].args)
	}
	_ = m
}

func TestEmptyAndDisconnectedStates(t *testing.T) {
	m, _ := New("w1:p9", &fakeCaller{}).Update(SnapshotMsg(snap()))
	if !strings.Contains(m.View(), "prefix+d") {
		t.Fatal("empty pane should hint how to add dots")
	}
	m, _ = m.Update(DisconnectedMsg{})
	if !strings.Contains(m.View(), "engine down") {
		t.Fatal("disconnected banner")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/pane/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/ui/style/style.go`**

```go
// Package style holds the colors and text styles shared by the Dots TUIs.
package style

import "github.com/charmbracelet/lipgloss"

var palette = map[string]lipgloss.Color{
	"red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
}

func Color(name string) lipgloss.Color {
	if c, ok := palette[name]; ok {
		return c
	}
	return "7"
}

var (
	Title    = lipgloss.NewStyle().Bold(true)
	Dim      = lipgloss.NewStyle().Faint(true)
	Selected = lipgloss.NewStyle().Reverse(true)
	Err      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func Glyph(glyph, color string) string {
	return lipgloss.NewStyle().Foreground(Color(color)).Render(glyph)
}

func Severity(sev string) lipgloss.Style {
	switch sev {
	case "critical":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	case "warn":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
}
```

- [ ] **Step 4: Write `internal/ui/pane/model.go`**

```go
// Package pane is the Dots side pane: roster, approval queue and feed for one
// main agent pane.
package pane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type SnapshotMsg view.Snapshot
type DisconnectedMsg struct{}
type errMsg struct{ err error }

type area int

const (
	areaQueue area = iota
	areaRoster
)

type Model struct {
	target    string
	caller    Caller
	snap      view.Snapshot
	connected bool
	area      area
	roster    int
	card      int
	showFeed  bool
	showHelp  bool
	editing   bool
	editID    string
	editor    textarea.Model
	asking    bool
	askSlug   string
	question  textinput.Model
	width     int
	status    string
}

func New(target string, c Caller) Model {
	ta := textarea.New()
	ta.SetHeight(6)
	ta.ShowLineNumbers = false
	ti := textinput.New()
	ti.Placeholder = "optional question, enter to summon"
	return Model{target: target, caller: c, editor: ta, question: ti}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) pane() view.Pane {
	p, _ := m.snap.Pane(m.target)
	return p
}

func (m Model) selectedCard() (queue.Card, bool) {
	p := m.pane()
	if m.card >= 0 && m.card < len(p.Cards) {
		return p.Cards[m.card], true
	}
	return queue.Card{}, false
}

func (m Model) selectedDot() (view.DotState, bool) {
	p := m.pane()
	if m.roster >= 0 && m.roster < len(p.Dots) {
		return p.Dots[m.roster], true
	}
	return view.DotState{}, false
}

func sendable(c queue.Card) bool {
	return (c.Status == queue.StatusPending || c.Status == queue.StatusAuto) && c.Mode != string(dots.ModeSuggest)
}

func (m *Model) clamp() {
	p := m.pane()
	m.card = max(0, min(m.card, len(p.Cards)-1))
	m.roster = max(0, min(m.roster, len(p.Dots)-1))
}

func (m *Model) move(d int) {
	if m.area == areaRoster {
		m.roster += d
	} else {
		m.card += d
	}
	m.clamp()
}

func nextTarget(s view.Snapshot, cur string) string {
	for i, p := range s.Panes {
		if p.PaneID == cur && len(s.Panes) > 0 {
			return s.Panes[(i+1)%len(s.Panes)].PaneID
		}
	}
	if len(s.Panes) > 0 {
		return s.Panes[0].PaneID
	}
	return cur
}

func (m Model) call(cmd string, args any) tea.Cmd {
	c := m.caller
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Call(ctx, cmd, args); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.editor.SetWidth(max(20, msg.Width-2))
		return m, nil
	case SnapshotMsg:
		m.snap, m.connected = view.Snapshot(msg), true
		if m.status == "engine down, restarting…" {
			m.status = ""
		}
		m.clamp()
		return m, nil
	case DisconnectedMsg:
		m.connected = false
		m.status = "engine down, restarting…"
		return m, nil
	case errMsg:
		m.status = msg.err.Error()
		return m, nil
	case tea.KeyMsg:
		switch {
		case m.editing:
			return m.updateEditor(msg)
		case m.asking:
			return m.updateQuestion(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateEditor(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.editing = false
		return m, nil
	case "ctrl+s":
		m.editing = false
		return m, m.call(ipc.CmdEdit, ipc.CardArgs{ID: m.editID, Body: m.editor.Value()})
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	return m, cmd
}

func (m Model) updateQuestion(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.asking = false
		return m, nil
	case "enter":
		m.asking = false
		m.status = "summoned " + m.askSlug
		return m, m.call(ipc.CmdSummon, ipc.SummonArgs{PaneID: m.target, Slug: m.askSlug, Question: strings.TrimSpace(m.question.Value())})
	}
	var cmd tea.Cmd
	m.question, cmd = m.question.Update(k)
	return m, cmd
}

func (m Model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
	case "f":
		m.showFeed = !m.showFeed
	case "tab":
		if m.area == areaQueue {
			m.area = areaRoster
		} else {
			m.area = areaQueue
		}
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "t":
		m.target = nextTarget(m.snap, m.target)
		m.roster, m.card = 0, 0
	case "enter":
		c, ok := m.selectedCard()
		if !ok {
			return m, nil
		}
		if c.Mode == string(dots.ModeSuggest) {
			m.status = "suggestions can't be sent: copy the text instead"
			return m, nil
		}
		if sendable(c) {
			return m, m.call(ipc.CmdSend, ipc.SendArgs{IDs: []string{c.ID}})
		}
	case "a":
		var ids []string
		for _, c := range m.pane().Cards {
			if sendable(c) {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			return m, m.call(ipc.CmdSend, ipc.SendArgs{IDs: ids})
		}
	case "x":
		if c, ok := m.selectedCard(); ok {
			return m, m.call(ipc.CmdDismiss, ipc.CardArgs{ID: c.ID})
		}
	case "e":
		if c, ok := m.selectedCard(); ok && sendable(c) {
			m.editing, m.editID = true, c.ID
			m.editor.SetValue(c.Body)
			m.editor.CursorEnd()
			m.editor.Focus()
			return m, textarea.Blink
		}
	case "s":
		if d, ok := m.selectedDot(); ok {
			m.asking, m.askSlug = true, d.Slug
			m.question.SetValue("")
			m.question.Focus()
			return m, textinput.Blink
		}
	case "m":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdSetMode, ipc.SetModeArgs{Slug: d.Slug, Mode: string(dots.Mode(d.Mode).Next())})
		}
	case "p":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdPause, ipc.PauseArgs{PaneID: m.target, Slug: d.Slug, Paused: !d.Paused})
		}
	case "d":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdDetach, ipc.DotArgs{PaneID: m.target, Slug: d.Slug})
		}
	case "r":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdReset, ipc.DotArgs{PaneID: m.target, Slug: d.Slug})
		}
	}
	return m, nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func rule(title string, w int) string {
	return style.Dim.Render("── "+title+" "+strings.Repeat("─", max(0, w-len(title)-4))) + "\n"
}

const helpText = `j/k move   tab roster/queue   t next agent
⏎ send card   a send all   e edit   x dismiss
s summon dot   m cycle mode   p pause   d detach
r reset errored dot   f feed   ? help   q close`

func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 46
	}
	var b strings.Builder
	p, ok := m.snap.Pane(m.target)
	agent := p.Agent
	if agent == "" {
		agent = "agent"
	}
	head := fmt.Sprintf("DOTS → %s (%s)", agent, m.target)
	if ok {
		head += fmt.Sprintf("  budget %d/%d", p.Calls, p.Budget)
	}
	if m.snap.PausedAll {
		head += "  ⏸ all paused"
	}
	b.WriteString(style.Title.Render(head) + "\n")
	if p.Source == "scrollback" {
		b.WriteString(style.Dim.Render("(reading scrollback: transcript unavailable)") + "\n")
	}
	if !m.connected && m.status != "" {
		b.WriteString(style.Err.Render(m.status) + "\n")
	}
	if len(p.Dots) == 0 {
		b.WriteString(style.Dim.Render("No dots here yet. Press prefix+d to add some.") + "\n")
	}
	for i, d := range p.Dots {
		line := fmt.Sprintf("%s %-12s %-8s %s", style.Glyph(d.Glyph, d.Color), trunc(d.Name, 12), d.Mode, d.Status)
		if m.area == areaRoster && i == m.roster {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(rule(fmt.Sprintf("QUEUE (%d)", len(p.Cards)), w))
	body := lipgloss.NewStyle().Width(max(10, w-2))
	for i, c := range p.Cards {
		sel := m.area == areaQueue && i == m.card
		marker := " "
		if sel {
			marker = "▸"
		}
		tags := ""
		switch {
		case c.Status == queue.StatusOrphaned:
			tags = " [orphaned]"
		case c.Status == queue.StatusAuto:
			tags = " [auto]"
		case c.Mode == string(dots.ModeSuggest):
			tags = " [suggestion]"
		}
		b.WriteString(fmt.Sprintf("%s%s %s  %s · turn %d%s\n", marker, c.Glyph, style.Severity(c.Severity).Render(strings.ToUpper(c.Severity)), c.DotName, c.Turn, tags))
		text := c.Body
		if !sel {
			text = trunc(strings.ReplaceAll(text, "\n", " "), max(20, w*2))
		}
		b.WriteString(body.Render("  "+text) + "\n")
		if c.Error != "" {
			b.WriteString(style.Err.Render("  delivery failed: "+c.Error) + "\n")
		}
		if sel {
			if c.Mode == string(dots.ModeSuggest) {
				b.WriteString(style.Dim.Render("  x dismiss · copy the text to use it") + "\n")
			} else {
				b.WriteString(style.Dim.Render("  ⏎ send  e edit  x dismiss") + "\n")
			}
		}
	}
	if m.editing {
		b.WriteString(rule("EDIT", w) + m.editor.View() + "\n" + style.Dim.Render("ctrl+s save · esc cancel") + "\n")
	}
	if m.asking {
		b.WriteString(rule("SUMMON "+m.askSlug, w) + m.question.View() + "\n")
	}
	if m.showFeed {
		b.WriteString(rule("FEED", w))
		for _, e := range p.Feed {
			line := fmt.Sprintf("%3d  %-10s %s", e.Turn, trunc(e.Dot, 10), e.Outcome)
			if e.Detail != "" && e.Outcome == "error" {
				line += "  " + trunc(e.Detail, max(10, w-24))
			}
			b.WriteString(line + "\n")
		}
	}
	if m.showHelp {
		b.WriteString(rule("HELP", w) + helpText + "\n")
	}
	footer := "j/k move · ⏎ send · a all · s summon · ? help"
	if m.connected && m.status != "" {
		footer = m.status
	}
	b.WriteString(style.Dim.Render(footer))
	return b.String()
}
```

- [ ] **Step 5: Write `cmd/herdr-dots/ui.go`**

```go
package main

import (
	"context"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/ui/pane"
	"herdr-dots/internal/view"
)

// envTarget carries the main agent pane id into the panes our actions open.
const envTarget = "HERDR_DOTS_TARGET"

func resolveTarget() string {
	if t := os.Getenv(envTarget); t != "" {
		return t
	}
	ic, _ := herdr.ParseInvocationContext(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	return ic.FocusedPaneID
}

// uiSetup resolves paths, makes sure the engine runs, and returns its client.
func uiSetup() (paths.Paths, ipc.Client, error) {
	p, err := paths.FromEnv()
	if err != nil {
		return p, ipc.Client{}, err
	}
	if err := p.Ensure(); err != nil {
		return p, ipc.Client{}, err
	}
	if err := ensureEngine(p); err != nil {
		return p, ipc.Client{}, err
	}
	return p, ipc.Client{Path: p.EngineSock()}, nil
}

// pumpSnapshots forwards engine snapshots to a program, restarting the engine
// and reconnecting whenever the stream drops.
func pumpSnapshots(ctx context.Context, p paths.Paths, cl ipc.Client, send func(tea.Msg), wrap func(view.Snapshot) tea.Msg, disconnected tea.Msg) {
	for ctx.Err() == nil {
		if ch, err := cl.Watch(ctx); err == nil {
			for s := range ch {
				send(wrap(s))
			}
		}
		if ctx.Err() != nil {
			return
		}
		send(disconnected)
		_ = ensureEngine(p)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func runPane([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	prog := tea.NewProgram(pane.New(resolveTarget(), cl), tea.WithAltScreen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return pane.SnapshotMsg(s) }, pane.DisconnectedMsg{})
	_, err = prog.Run()
	return err
}
```

In `cmd/herdr-dots/main.go`, change `"pane": notYet("pane"),` to `"pane": runPane,`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/ui/... && go build ./... && go vet ./...`
Expected: PASS. If `TestDismissEditSummonModePause` fails on the edit body, check that `CursorEnd()` put the cursor after the existing text before `!` was typed.

- [ ] **Step 7: Commit**

```bash
git add internal/ui cmd/herdr-dots
git commit -m "feat: Dots side pane TUI with roster, approval queue, feed and keys"
```

---

### Task 19: Manage popup — library, templates and dot editor

**Files:**
- Create: `internal/ui/manage/form.go`, `internal/ui/manage/model.go`
- Modify: `cmd/herdr-dots/ui.go` (add `runManage`), `cmd/herdr-dots/main.go` (register `runManage`)
- Test: `internal/ui/manage/form_test.go`, `internal/ui/manage/model_test.go`

**Interfaces:**
- Consumes: `dots` (model, `Templates`, `Load`, `Save`, `Delete`, `ErrExists`, `Colors`, `BackendKinds`, `Verbosities`, `Slugify`), `backend.New(...).Check`, ipc, view, style.
- Produces:
  - `manage.NewForm(d dots.Dot, isNew bool) *Form`; `(*Form) Dot() (dots.Dot, error)`; `(*Form) IsNew() bool`; `(*Form) SetError(string)`; `(*Form) Update(tea.KeyMsg) tea.Cmd`; `(*Form) View(width int) string`
  - `manage.Deps{Caller Caller; DotsDir string; Check func(ctx context.Context, kind string) error; NewDot func() dots.Dot}` (`NewDot` nil → `dots.New("")`; the real one applies `default_backend`/`default_model` from `config.toml`); `manage.New(target string, d Deps) Model`; `manage.SnapshotMsg`
  - `cmd`: `runManage(args []string) error`

- [ ] **Step 1: Write the failing tests**

`internal/ui/manage/form_test.go`:
```go
package manage

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
)

func k(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func focusOn(f *Form, label string) {
	for f.fields[f.focus].label != label {
		f.Update(k("tab"))
	}
}

func TestFormRoundTripsATemplate(t *testing.T) {
	d, _ := dots.FromTemplate("security")
	d.Trigger.FileGlobs = []string{"**/*.ts", "api/**"}
	d.Backend.Args = []string{"--fallback-model", "haiku"}
	got, err := NewForm(d, false).Dot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("round trip changed the dot:\n got %+v\nwant %+v", got, d)
	}
}

func TestFormEditsFields(t *testing.T) {
	f := NewForm(dots.New(""), true)
	for _, r := range "Perf Hawk" {
		f.Update(k(string(r)))
	}
	focusOn(f, "Mode")
	f.Update(k("right")) // queue → inject
	focusOn(f, "Review every turn")
	f.Update(k("space"))
	focusOn(f, "File globs")
	for _, r := range "a.go, b/** ,," {
		f.Update(k(string(r)))
	}
	focusOn(f, "Purpose")
	for _, r := range "Find slow code." {
		f.Update(k(string(r)))
	}
	d, err := f.Dot()
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Perf Hawk" || d.Slug != "perf-hawk" || d.Delivery.Mode != dots.ModeInject || d.Trigger.OnTurn || d.Purpose != "Find slow code." {
		t.Fatalf("dot %+v", d)
	}
	if !reflect.DeepEqual(d.Trigger.FileGlobs, []string{"a.go", "b/**"}) {
		t.Fatalf("globs %v", d.Trigger.FileGlobs)
	}
}

func TestFormRejectsNonNumbers(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	focusOn(f, "Context turns")
	f.Update(k("backspace"))
	f.Update(k("z"))
	if _, err := f.Dot(); err == nil || !strings.Contains(err.Error(), "Context turns") {
		t.Fatalf("want number error, got %v", err)
	}
}
```

`internal/ui/manage/model_test.go`:
```go
package manage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

type fakeCaller struct{ calls []string }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, cmd+" "+string(b))
	return nil
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press runs returned commands (and the messages they produce) like the runtime.
func press(m tea.Model, keys ...string) tea.Model {
	for _, s := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(s))
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				break
			}
			if _, quit := msg.(tea.QuitMsg); quit {
				break
			}
			m, cmd = m.Update(msg)
		}
	}
	return m
}

func lib() view.Snapshot {
	return view.Snapshot{
		Library: []view.LibraryDot{
			{Slug: "backend", Name: "Backend", Glyph: "▼", Backend: "claude-cli", Model: "sonnet", Mode: "queue"},
			{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"},
		},
		Panes:   []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{{Slug: "security"}}}},
		Invalid: []view.Invalid{{File: "broken.toml", Line: 2, Error: "bad"}},
	}
}

func setup(t *testing.T, target string) (tea.Model, *fakeCaller, string) {
	dir := t.TempDir()
	fc := &fakeCaller{}
	m, _ := New(target, Deps{Caller: fc, DotsDir: dir, Check: func(context.Context, string) error { return nil }}).Update(SnapshotMsg(lib()))
	return m, fc, dir
}

func TestLibraryShowsAttachmentAndInvalid(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	v := m.View()
	for _, want := range []string{"target: w1:p1", "[ ] ▼ Backend", "[x] ◆ Security", "broken.toml:2"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q\n%s", want, v)
		}
	}
}

func TestApplyAttachesAndDetaches(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	press(m, "space", "j", "space", "enter") // check Backend, uncheck Security
	want := []string{
		ipc.CmdAttach + ` {"pane_id":"w1:p1","slugs":["backend"]}`,
		ipc.CmdDetach + ` {"pane_id":"w1:p1","slug":"security"}`,
	}
	if strings.Join(fc.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(fc.calls, "\n"))
	}
}

func TestNoTargetIsLibraryOnly(t *testing.T) {
	m, fc, _ := setup(t, "")
	m = press(m, "space")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "focus an agent pane") {
		t.Fatal("space without a target must explain, not attach")
	}
}

func TestCreateFromTemplateSavesAndReloads(t *testing.T) {
	m, fc, dir := setup(t, "w1:p1")
	m = press(m, "t")
	if !strings.Contains(m.View(), "Copywriter") {
		t.Fatalf("templates screen:\n%s", m.View())
	}
	// Templates are sorted by name: Backend, Copywriter, ... — pick Copywriter.
	m = press(m, "j", "enter", "ctrl+s")
	if _, err := os.Stat(filepath.Join(dir, "copywriter.toml")); err != nil {
		t.Fatalf("dot file not written: %v\n%s", err, m.View())
	}
	if last := fc.calls[len(fc.calls)-1]; !strings.HasPrefix(last, ipc.CmdReloadDots) {
		t.Fatalf("want reload after save, got %v", fc.calls)
	}
	if !strings.Contains(m.View(), "saved copywriter") {
		t.Fatalf("status:\n%s", m.View())
	}
}

func TestDuplicateNameShowsError(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("security")
	_ = dots.Save(dir, d, true)
	m = press(m, "t")
	for i := 0; i < len(dots.Templates()); i++ {
		if strings.Contains(m.View(), "▸ ◆ Security") {
			break
		}
		m = press(m, "j")
	}
	m = press(m, "enter", "ctrl+s")
	if !strings.Contains(m.View(), "already exists") {
		t.Fatalf("want duplicate error:\n%s", m.View())
	}
}

func TestNewDotUsesConfigDefaults(t *testing.T) {
	fc := &fakeCaller{}
	newDot := func() dots.Dot { d := dots.New(""); d.Backend.Kind, d.Backend.Model = "codex-cli", "gpt-5"; return d }
	var m tea.Model = New("w1:p1", Deps{Caller: fc, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil }, NewDot: newDot})
	m, _ = m.Update(SnapshotMsg(lib()))
	m = press(m, "n")
	if v := m.View(); !strings.Contains(v, "codex-cli") || !strings.Contains(v, "gpt-5") {
		t.Fatalf("editor should start from config defaults:\n%s", v)
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	m, fc, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	m = press(m, "D")
	if !strings.Contains(m.View(), "Delete Backend?") {
		t.Fatalf("confirm:\n%s", m.View())
	}
	press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "backend.toml")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
	if !strings.HasPrefix(fc.calls[len(fc.calls)-1], ipc.CmdReloadDots) {
		t.Fatal("reload after delete")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/manage/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/ui/manage/form.go`**

```go
package manage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ui/style"
)

type fieldKind int

const (
	kText fieldKind = iota
	kArea
	kChoice
	kBool
	kInt
	kList
)

type field struct {
	label   string
	kind    fieldKind
	input   textinput.Model
	area    textarea.Model
	choices []string
	idx     int
	on      bool
}

// Form edits every setting of one dot. orig keeps fields the form doesn't
// show (backend args), so saving never drops them.
type Form struct {
	orig   dots.Dot
	isNew  bool
	fields []field
	focus  int
	err    string
}

func textField(label, v string) field {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.SetValue(v)
	return field{label: label, kind: kText, input: ti}
}

func intField(label string, v int) field {
	f := textField(label, strconv.Itoa(v))
	f.kind = kInt
	return f
}

func listField(label string, v []string) field {
	f := textField(label, strings.Join(v, ", "))
	f.kind = kList
	return f
}

func choiceField(label string, choices []string, v string) field {
	return field{label: label, kind: kChoice, choices: choices, idx: max(0, slices.Index(choices, v))}
}

func NewForm(d dots.Dot, isNew bool) *Form {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(6)
	ta.SetWidth(64)
	ta.CharLimit = 4000
	ta.SetValue(d.Purpose)
	modes := make([]string, len(dots.Modes))
	for i, m := range dots.Modes {
		modes[i] = string(m)
	}
	f := &Form{orig: d, isNew: isNew, fields: []field{
		textField("Name", d.Name),
		textField("Glyph", d.Glyph),
		choiceField("Color", dots.Colors, d.Color),
		{label: "Purpose", kind: kArea, area: ta},
		choiceField("Backend", dots.BackendKinds, d.Backend.Kind),
		textField("Model", d.Backend.Model),
		choiceField("Mode", modes, string(d.Delivery.Mode)),
		{label: "Review every turn", kind: kBool, on: d.Trigger.OnTurn},
		listField("File globs", d.Trigger.FileGlobs),
		intField("Cooldown (turns)", d.Trigger.Cooldown),
		intField("Context turns", d.Context.Turns),
		{label: "Read repo (read-only)", kind: kBool, on: d.Context.RepoRead},
		choiceField("Verbosity", dots.Verbosities, d.Context.Verbosity),
		intField("Budget (calls/session)", d.Budget.MaxCallsPerSession),
	}}
	f.setFocus(0)
	return f
}

func (f *Form) IsNew() bool        { return f.isNew }
func (f *Form) SetError(s string)  { f.err = s }

func (f *Form) setFocus(i int) {
	n := len(f.fields)
	f.focus = ((i % n) + n) % n
	for j := range f.fields {
		fl := &f.fields[j]
		fl.input.Blur()
		fl.area.Blur()
		if j != f.focus {
			continue
		}
		switch fl.kind {
		case kText, kInt, kList:
			fl.input.Focus()
			fl.input.CursorEnd()
		case kArea:
			fl.area.Focus()
		}
	}
}

func (f *Form) Update(k tea.KeyMsg) tea.Cmd {
	cur := &f.fields[f.focus]
	switch k.String() {
	case "tab":
		f.setFocus(f.focus + 1)
		return nil
	case "shift+tab":
		f.setFocus(f.focus - 1)
		return nil
	case "up":
		if cur.kind != kArea {
			f.setFocus(f.focus - 1)
			return nil
		}
	case "down":
		if cur.kind != kArea {
			f.setFocus(f.focus + 1)
			return nil
		}
	}
	var cmd tea.Cmd
	switch cur.kind {
	case kChoice:
		switch k.String() {
		case "right", "l", " ":
			cur.idx = (cur.idx + 1) % len(cur.choices)
		case "left", "h":
			cur.idx = (cur.idx - 1 + len(cur.choices)) % len(cur.choices)
		}
	case kBool:
		switch k.String() {
		case " ", "enter", "left", "right":
			cur.on = !cur.on
		}
	case kArea:
		cur.area, cmd = cur.area.Update(k)
	default:
		cur.input, cmd = cur.input.Update(k)
	}
	return cmd
}

func (f *Form) val(i int) string { return strings.TrimSpace(f.fields[i].input.Value()) }

func (f *Form) num(i int) (int, error) {
	n, err := strconv.Atoi(f.val(i))
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", f.fields[i].label)
	}
	return n, nil
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// field indexes, in NewForm order
const (
	iName = iota
	iGlyph
	iColor
	iPurpose
	iBackend
	iModel
	iMode
	iOnTurn
	iGlobs
	iCooldown
	iTurns
	iRepoRead
	iVerbosity
	iBudget
)

func (f *Form) Dot() (dots.Dot, error) {
	d := f.orig
	fl := f.fields
	d.Name = f.val(iName)
	d.Glyph = f.val(iGlyph)
	d.Color = fl[iColor].choices[fl[iColor].idx]
	d.Purpose = strings.TrimSpace(fl[iPurpose].area.Value())
	d.Backend.Kind = fl[iBackend].choices[fl[iBackend].idx]
	d.Backend.Model = f.val(iModel)
	d.Delivery.Mode = dots.Mode(fl[iMode].choices[fl[iMode].idx])
	d.Trigger.OnTurn = fl[iOnTurn].on
	d.Trigger.FileGlobs = splitList(f.fields[iGlobs].input.Value())
	d.Context.RepoRead = fl[iRepoRead].on
	d.Context.Verbosity = fl[iVerbosity].choices[fl[iVerbosity].idx]
	var err error
	if d.Trigger.Cooldown, err = f.num(iCooldown); err != nil {
		return d, err
	}
	if d.Context.Turns, err = f.num(iTurns); err != nil {
		return d, err
	}
	if d.Budget.MaxCallsPerSession, err = f.num(iBudget); err != nil {
		return d, err
	}
	if f.isNew {
		d.Slug = dots.Slugify(d.Name)
	}
	return d, nil
}

func (f *Form) View(width int) string {
	var b strings.Builder
	title := "EDIT DOT"
	if f.isNew {
		title = "NEW DOT"
	}
	b.WriteString(style.Title.Render(title) + "\n\n")
	for i, fl := range f.fields {
		label := fmt.Sprintf("%-24s", fl.label)
		if i == f.focus {
			label = style.Selected.Render(label)
		}
		var v string
		switch fl.kind {
		case kChoice:
			v = "‹ " + fl.choices[fl.idx] + " ›"
		case kBool:
			v = "[ ]"
			if fl.on {
				v = "[x]"
			}
		case kArea:
			v = "\n" + fl.area.View()
		default:
			v = fl.input.View()
		}
		b.WriteString(label + " " + v + "\n")
	}
	if f.err != "" {
		b.WriteString("\n" + style.Err.Render(f.err) + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("tab/shift+tab move · ←/→ change · space toggle · ctrl+s save · esc cancel"))
	_ = width
	return b.String()
}
```

- [ ] **Step 4: Write `internal/ui/manage/model.go`**

```go
// Package manage is the `prefix+d` popup: the dot library, templates, the dot
// editor, and attaching/detaching dots to the focused agent pane.
package manage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type Deps struct {
	Caller  Caller
	DotsDir string
	Check   func(ctx context.Context, kind string) error // backend CLI present?
	NewDot  func() dots.Dot                              // blank dot with config defaults; nil → dots.New("")
}

type SnapshotMsg view.Snapshot

type (
	doneMsg      struct{}
	errMsg       struct{ err error }
	formErrMsg   struct{ err error }
	savedMsg     struct{ slug string }
	deletedMsg   struct{ name string }
	reloadedMsg  struct{}
)

type screen int

const (
	scrLibrary screen = iota
	scrTemplates
	scrEditor
	scrConfirm
)

type Model struct {
	d         Deps
	target    string
	snap      view.Snapshot
	loaded    bool
	scr       screen
	cursor    int
	tcursor   int
	selected  map[string]bool
	attached  map[string]bool
	templates []dots.Dot
	form      *Form
	status    string
	width     int
}

func New(target string, d Deps) Model {
	return Model{d: d, target: target, selected: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates()}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) call(cmd string, args any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return m.d.Caller.Call(ctx, cmd, args)
}

func (m Model) reload() tea.Cmd {
	return func() tea.Msg {
		if err := m.call(ipc.CmdReloadDots, nil); err != nil {
			return errMsg{err}
		}
		return reloadedMsg{}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case SnapshotMsg:
		m.snap = view.Snapshot(msg)
		m.attached = map[string]bool{}
		if p, ok := m.snap.Pane(m.target); ok {
			for _, d := range p.Dots {
				m.attached[d.Slug] = true
			}
		}
		if !m.loaded {
			for s := range m.attached {
				m.selected[s] = true
			}
			m.loaded = true
		}
		m.cursor = max(0, min(m.cursor, len(m.snap.Library)-1))
	case errMsg:
		m.status = msg.err.Error()
	case formErrMsg:
		if m.form != nil {
			m.form.SetError(msg.err.Error())
		}
	case savedMsg:
		m.scr, m.form = scrLibrary, nil
		m.status = "saved " + msg.slug
		if m.target != "" {
			m.selected[msg.slug] = true
		}
		return m, m.reload()
	case deletedMsg:
		m.scr = scrLibrary
		m.status = "deleted " + msg.name
		return m, m.reload()
	case reloadedMsg:
	case doneMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch m.scr {
		case scrTemplates:
			return m.keysTemplates(msg)
		case scrEditor:
			return m.keysEditor(msg)
		case scrConfirm:
			return m.keysConfirm(msg)
		}
		return m.keysLibrary(msg)
	}
	return m, nil
}

func (m Model) current() (view.LibraryDot, bool) {
	if m.cursor >= 0 && m.cursor < len(m.snap.Library) {
		return m.snap.Library[m.cursor], true
	}
	return view.LibraryDot{}, false
}

func (m Model) keysLibrary(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "esc", "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.snap.Library)-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case " ":
		if m.target == "" {
			m.status = "focus an agent pane to attach dots"
			return m, nil
		}
		if d, ok := m.current(); ok {
			m.selected[d.Slug] = !m.selected[d.Slug]
		}
	case "enter":
		if m.target == "" {
			return m, tea.Quit
		}
		return m, m.apply()
	case "n":
		blank := dots.New("")
		if m.d.NewDot != nil {
			blank = m.d.NewDot()
		}
		m.form, m.scr = NewForm(blank, true), scrEditor
	case "t":
		m.scr, m.tcursor = scrTemplates, 0
	case "e":
		if d, ok := m.current(); ok {
			full, err := dots.Load(m.d.DotsDir, d.Slug)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.form, m.scr = NewForm(full, false), scrEditor
		}
	case "D":
		if _, ok := m.current(); ok {
			m.scr = scrConfirm
		}
	}
	return m, nil
}

// apply attaches newly checked dots in one call and detaches unchecked ones.
func (m Model) apply() tea.Cmd {
	var add []string
	var remove []string
	for _, d := range m.snap.Library {
		switch {
		case m.selected[d.Slug] && !m.attached[d.Slug]:
			add = append(add, d.Slug)
		case !m.selected[d.Slug] && m.attached[d.Slug]:
			remove = append(remove, d.Slug)
		}
	}
	return func() tea.Msg {
		if len(add) > 0 {
			if err := m.call(ipc.CmdAttach, ipc.AttachArgs{PaneID: m.target, Slugs: add}); err != nil {
				return errMsg{err}
			}
		}
		for _, s := range remove {
			if err := m.call(ipc.CmdDetach, ipc.DotArgs{PaneID: m.target, Slug: s}); err != nil {
				return errMsg{err}
			}
		}
		return doneMsg{}
	}
}

func (m Model) keysTemplates(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.scr = scrLibrary
	case "j", "down":
		m.tcursor = min(m.tcursor+1, len(m.templates)-1)
	case "k", "up":
		m.tcursor = max(m.tcursor-1, 0)
	case "enter":
		m.form, m.scr = NewForm(m.templates[m.tcursor], true), scrEditor
	}
	return m, nil
}

func (m Model) keysEditor(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.form, m.scr = nil, scrLibrary
		return m, nil
	case "ctrl+s":
		return m, m.save()
	}
	return m, m.form.Update(k)
}

func (m Model) save() tea.Cmd {
	f, dir, check := m.form, m.d.DotsDir, m.d.Check
	return func() tea.Msg {
		d, err := f.Dot()
		if err != nil {
			return formErrMsg{err}
		}
		if err := d.Validate(); err != nil {
			return formErrMsg{err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := check(ctx, d.Backend.Kind); err != nil {
			return formErrMsg{err}
		}
		if err := dots.Save(dir, d, f.IsNew()); err != nil {
			if errors.Is(err, dots.ErrExists) {
				return formErrMsg{fmt.Errorf("a dot named %q already exists: pick another name", d.Name)}
			}
			return formErrMsg{err}
		}
		return savedMsg{slug: dots.Slugify(d.Name)}
	}
}

func (m Model) keysConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d, _ := m.current()
	switch k.String() {
	case "y":
		dir := m.d.DotsDir
		return m, func() tea.Msg {
			if err := dots.Delete(dir, d.Slug); err != nil {
				return errMsg{err}
			}
			return deletedMsg{name: d.Name}
		}
	default:
		m.scr = scrLibrary
	}
	return m, nil
}

func (m Model) View() string {
	switch m.scr {
	case scrEditor:
		return m.form.View(m.width)
	case scrTemplates:
		var b strings.Builder
		b.WriteString(style.Title.Render("NEW DOT FROM TEMPLATE") + "\n\n")
		for i, d := range m.templates {
			marker := "  "
			if i == m.tcursor {
				marker = "▸ "
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", marker, d.Glyph, d.Name))
		}
		b.WriteString("\n" + style.Dim.Render("j/k move · enter use · esc back"))
		return b.String()
	case scrConfirm:
		d, _ := m.current()
		return style.Title.Render(fmt.Sprintf("Delete %s?", d.Name)) + "\n\n" + style.Dim.Render("y delete · any other key cancels")
	}
	var b strings.Builder
	head := "DOTS LIBRARY ─ target: " + m.target
	if m.target == "" {
		head = "DOTS LIBRARY ─ no agent pane focused (library only)"
	}
	b.WriteString(style.Title.Render(head) + "\n\n")
	if len(m.snap.Library) == 0 {
		b.WriteString(style.Dim.Render("No dots yet. Press t to start from a template or n for a new dot.") + "\n")
	}
	for i, d := range m.snap.Library {
		check := "[ ]"
		if m.selected[d.Slug] {
			check = "[x]"
		}
		line := fmt.Sprintf("%s %s %-14s %-22s %s", check, style.Glyph(d.Glyph, d.Color), d.Name, d.Backend+"/"+d.Model, d.Mode)
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for _, inv := range m.snap.Invalid {
		b.WriteString(style.Err.Render(fmt.Sprintf("⚠ %s:%d: %s", inv.File, inv.Line, inv.Error)) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("space toggle · enter apply · n new · t template · e edit · D delete · esc close"))
	return b.String()
}
```

The library row renders the glyph through `style.Glyph`, which adds ANSI color codes in a real terminal but none in tests (lipgloss detects no TTY), so the test strings like `"[x] ◆ Security"` match.

- [ ] **Step 5: Add `runManage` to `cmd/herdr-dots/ui.go` and register it**

Append to `cmd/herdr-dots/ui.go` (add imports `herdr-dots/internal/backend`, `herdr-dots/internal/config`, `herdr-dots/internal/dots` and `herdr-dots/internal/ui/manage`):
```go
func runManage([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	check := func(ctx context.Context, kind string) error {
		b, err := backend.New(kind, nil)
		if err != nil {
			return err
		}
		return b.Check(ctx)
	}
	cfg, _ := config.Load(p.ConfigFile()) // malformed config → defaults; the engine logs the error
	newDot := func() dots.Dot {
		d := dots.New("")
		d.Backend.Kind, d.Backend.Model = cfg.DefaultBackend, cfg.DefaultModel
		return d
	}
	prog := tea.NewProgram(manage.New(resolveTarget(), manage.Deps{Caller: cl, DotsDir: p.DotsDir(), Check: check, NewDot: newDot}), tea.WithAltScreen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return manage.SnapshotMsg(s) }, nil)
	_, err = prog.Run()
	return err
}
```
In `pumpSnapshots`, skip `send(disconnected)` when `disconnected == nil`:
```go
		if disconnected != nil {
			send(disconnected)
		}
```
In `main.go`, change `"manage": notYet("manage"),` to `"manage": runManage,`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./internal/ui/... && go build ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/manage cmd/herdr-dots
git commit -m "feat: manage popup with dot library, templates, editor and attach/detach"
```

---

### Task 20: Summon popup and plugin actions

**Files:**
- Create: `internal/ui/summon/model.go`, `cmd/herdr-dots/action.go`
- Modify: `cmd/herdr-dots/ui.go` (add `runSummonUI`), `cmd/herdr-dots/main.go` (register `runSummonUI`, `runAction`)
- Test: `internal/ui/summon/model_test.go`, `cmd/herdr-dots/action_test.go`

**Interfaces:**
- Consumes: view, ipc, herdr (`OpenPluginPane`, `ClosePluginPane`, `PaneGet`, `ParseInvocationContext`), `ensureEngine`, `resolveTarget`, `envTarget`, `fsutil.WriteFileAtomic`.
- Produces:
  - `summon.New(target string, c Caller) Model`; `summon.SnapshotMsg`
  - `cmd`: `runSummonUI(args []string) error`; `runAction(args []string) error`; `toggle(m map[string]string, target string, exists func(string) bool) (closeID string, open bool)`

- [ ] **Step 1: Write the failing tests**

`internal/ui/summon/model_test.go`:
```go
package summon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

type fakeCaller struct{ calls []string }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, cmd+" "+string(b))
	return nil
}

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPickDotAskAndSummon(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"}, {Slug: "copy", Name: "Copy", Glyph: "✎"},
	}}}}))
	if !strings.Contains(m.View(), "Copy") {
		t.Fatal(m.View())
	}
	for _, k := range []string{"j", "enter", "o", "k", "?", "enter"} {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, quit := msg.(tea.QuitMsg); !quit {
					m, _ = m.Update(msg)
				}
			}
		}
	}
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdSummon+` {"pane_id":"w1:p1","slug":"copy","question":"ok?"}` {
		t.Fatalf("calls %v", fc.calls)
	}
}

func TestNoDotsAttached(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	if !strings.Contains(m.View(), "prefix+d") {
		t.Fatal(m.View())
	}
}
```

`cmd/herdr-dots/action_test.go`:
```go
package main

import "testing"

func TestToggle(t *testing.T) {
	always := func(string) bool { return true }
	never := func(string) bool { return false }

	m := map[string]string{}
	if c, open := toggle(m, "w1:p1", always); c != "" || !open {
		t.Fatal("no Dots pane yet: open one")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p1", always); c != "w1:p7" || open || len(m) != 0 {
		t.Fatal("existing Dots pane for target: close it")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p7", always); c != "w1:p7" || open {
		t.Fatal("focused pane is a Dots pane: close it")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p1", never); c != "" || !open || len(m) != 0 {
		t.Fatal("stale mapping: forget it and open a new pane")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/summon/ ./cmd/herdr-dots/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/ui/summon/model.go`**

```go
// Package summon is the small popup that runs one attached dot right now,
// with an optional question.
package summon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type SnapshotMsg view.Snapshot
type errMsg struct{ err error }

type Model struct {
	target string
	caller Caller
	dots   []view.DotState
	cursor int
	asking bool
	input  textinput.Model
	status string
}

func New(target string, c Caller) Model {
	ti := textinput.New()
	ti.Placeholder = "optional question"
	return Model{target: target, caller: c, input: ti}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SnapshotMsg:
		p, _ := view.Snapshot(msg).Pane(m.target)
		m.dots = p.Dots
		m.cursor = max(0, min(m.cursor, len(m.dots)-1))
	case errMsg:
		m.status = msg.err.Error()
	case tea.KeyMsg:
		if m.asking {
			switch msg.String() {
			case "esc":
				m.asking = false
				return m, nil
			case "enter":
				d := m.dots[m.cursor]
				q := strings.TrimSpace(m.input.Value())
				c, target := m.caller, m.target
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := c.Call(ctx, ipc.CmdSummon, ipc.SummonArgs{PaneID: target, Slug: d.Slug, Question: q}); err != nil {
						return errMsg{err}
					}
					return tea.Quit()
				}
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.cursor = min(m.cursor+1, max(0, len(m.dots)-1))
		case "k", "up":
			m.cursor = max(m.cursor-1, 0)
		case "enter":
			if len(m.dots) > 0 {
				m.asking = true
				m.input.Focus()
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(style.Title.Render("SUMMON A DOT → "+m.target) + "\n\n")
	if len(m.dots) == 0 {
		b.WriteString(style.Dim.Render("No dots attached to this pane. Press prefix+d to add some.") + "\n")
	}
	for i, d := range m.dots {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Glyph(d.Glyph, d.Color), d.Name))
	}
	if m.asking {
		b.WriteString("\n" + m.input.View() + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + style.Err.Render(m.status) + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("enter choose · enter again to summon · esc close"))
	return b.String()
}
```

- [ ] **Step 4: Write `cmd/herdr-dots/action.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/paths"
)

func runAction(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: herdr-dots action <manage|toggle-pane|summon|pause-all>")
	}
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return err
	}
	if err := ensureEngine(p); err != nil {
		return err
	}
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return err
	}
	target := resolveTarget()
	if target == "" {
		target = os.Getenv("HERDR_PANE_ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	env := map[string]string{envTarget: target}
	switch args[0] {
	case "manage":
		_, err = hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "manage", Placement: "popup", Width: "80%", Height: "80%", Env: env, Focus: true})
	case "summon":
		_, err = hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "summon", Placement: "popup", Width: "60%", Height: 14, Env: env, Focus: true})
	case "toggle-pane":
		err = togglePane(ctx, p, hc, target)
	case "pause-all":
		err = ipc.Client{Path: p.EngineSock()}.Call(ctx, ipc.CmdPauseAll, nil)
	default:
		err = fmt.Errorf("unknown action %q", args[0])
	}
	return err
}

// toggle decides what toggle-pane does. m maps main pane → its Dots pane and
// is updated in place.
func toggle(m map[string]string, target string, exists func(string) bool) (closeID string, open bool) {
	for t, id := range m {
		if id == target { // the focused pane is itself a Dots pane
			delete(m, t)
			return id, false
		}
	}
	if id, ok := m[target]; ok {
		delete(m, target)
		if exists(id) {
			return id, false
		}
	}
	return "", true
}

func togglePane(ctx context.Context, p paths.Paths, hc herdr.Client, target string) error {
	if target == "" {
		return errors.New("focus an agent pane first")
	}
	m := map[string]string{}
	data, err := os.ReadFile(p.DotsPanesFile())
	if err == nil {
		_ = json.Unmarshal(data, &m)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	closeID, open := toggle(m, target, func(id string) bool {
		_, err := hc.PaneGet(ctx, id)
		return err == nil
	})
	if closeID != "" {
		if err := hc.ClosePluginPane(ctx, closeID); err != nil && !herdr.IsNotFound(err) {
			return err
		}
	}
	if open {
		id, err := hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "pane", Placement: "split", Direction: "right",
			TargetPaneID: target, Env: map[string]string{envTarget: target}, Focus: false})
		if err != nil {
			return err
		}
		m[target] = id
	}
	out, _ := json.Marshal(m)
	return fsutil.WriteFileAtomic(p.DotsPanesFile(), out, 0o600)
}
```

The split opens at Herdr's default ratio. If Task 1 (V3) showed `pane.resize` narrows it reliably, note that as a follow-up in the README's known gaps; don't add it here.

- [ ] **Step 5: Add `runSummonUI` and register handlers**

Append to `cmd/herdr-dots/ui.go` (add import `herdr-dots/internal/ui/summon`):
```go
func runSummonUI([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	prog := tea.NewProgram(summon.New(resolveTarget(), cl))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return summon.SnapshotMsg(s) }, nil)
	_, err = prog.Run()
	return err
}
```
In `main.go`: `"summon-ui": runSummonUI,` and `"action": runAction,`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -race ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/summon cmd/herdr-dots
git commit -m "feat: summon popup and plugin actions (manage, toggle-pane, summon, pause-all)"
```

---

### Task 21: `setup-keys` helper

**Files:**
- Create: `internal/keys/keys.go`, `cmd/herdr-dots/setupkeys.go`
- Modify: `cmd/herdr-dots/main.go` (register `runSetupKeys`)
- Test: `internal/keys/keys_test.go`

**Interfaces:**
- Produces:
  - `keys.Binding{Key, Action, Description string}`; `keys.Defaults() []Binding`
  - `keys.BuiltinPrefixKeys` (Herdr 0.8.2 defaults)
  - `keys.ConfigPath(getenv func(string) string, home string) string`
  - `keys.Existing(data []byte) (map[string]string, error)` — key → what it's bound to
  - `keys.Conflict{Binding Binding; With string}`; `keys.Plan(want []Binding, existing map[string]string) (add, already []Binding, conflicts []Conflict)`
  - `keys.Block(bs []Binding) string`; `keys.Append(path string, bs []Binding, now time.Time) (backup string, err error)`
  - `cmd`: `runSetupKeys(args []string) error` with `--yes`

- [ ] **Step 1: Write the failing tests**

`internal/keys/keys_test.go`:
```go
package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const userConfig = `onboarding = false
[keys]
prefix = "ctrl+a"
settings = "prefix+t"

[[keys.command]]
key = "prefix+up"
type = "plugin_action"
command = "cloudmanic.herdr-plus.projects"

[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "someone.else.thing"
`

func TestConfigPath(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if p := ConfigPath(get, "/h"); p != "/h/.config/herdr/config.toml" {
		t.Fatal(p)
	}
	env["XDG_CONFIG_HOME"] = "/x"
	if p := ConfigPath(get, "/h"); p != "/x/herdr/config.toml" {
		t.Fatal(p)
	}
	env["HERDR_CONFIG_PATH"] = "/custom.toml"
	if p := ConfigPath(get, "/h"); p != "/custom.toml" {
		t.Fatal(p)
	}
}

func TestExistingReadsCommandsAndKeyTable(t *testing.T) {
	ex, err := Existing([]byte(userConfig))
	if err != nil {
		t.Fatal(err)
	}
	if ex["prefix+up"] != "cloudmanic.herdr-plus.projects" || ex["prefix+t"] != "herdr: settings" || ex["prefix+a"] != "someone.else.thing" {
		t.Fatalf("existing %v", ex)
	}
	if _, ok := ex["ctrl+a"]; ok {
		t.Fatal("the prefix itself is not a binding")
	}
}

func TestPlanSkipsConflictsAndAlreadyBound(t *testing.T) {
	ex, _ := Existing([]byte(userConfig + "\n[[keys.command]]\nkey = \"prefix+d\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n"))
	add, already, conflicts := Plan(Defaults(), ex)
	if len(already) != 1 || already[0].Key != "prefix+d" {
		t.Fatalf("already %v", already)
	}
	if len(conflicts) != 1 || conflicts[0].Binding.Key != "prefix+a" || conflicts[0].With != "someone.else.thing" {
		t.Fatalf("conflicts %v", conflicts)
	}
	if len(add) != 1 || add[0].Key != "prefix+shift+s" {
		t.Fatalf("add %v", add)
	}
}

func TestPlanTreatsHerdrBuiltinsAsTaken(t *testing.T) {
	_, _, conflicts := Plan([]Binding{{Key: "prefix+s", Action: "herdr-dots.summon"}, {Key: "prefix+shift+d", Action: "x"}}, map[string]string{})
	if len(conflicts) != 2 {
		t.Fatalf("built-ins must conflict: %v", conflicts)
	}
}

func TestBlockIsValidTOMLAndAppendBacksUp(t *testing.T) {
	block := Block(Defaults())
	var parsed struct {
		Keys struct {
			Command []map[string]string `toml:"command"`
		} `toml:"keys"`
	}
	if _, err := toml.Decode(block, &parsed); err != nil || len(parsed.Keys.Command) != 3 || parsed.Keys.Command[0]["type"] != "plugin_action" {
		t.Fatalf("block:\n%s\nerr %v", block, err)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(userConfig), 0o600)
	backup, err := Append(p, Defaults()[2:], time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(backup)
	if string(orig) != userConfig {
		t.Fatal("backup must hold the original")
	}
	now, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(now), userConfig) || !strings.Contains(string(now), `key = "prefix+shift+s"`) {
		t.Fatalf("appended:\n%s", now)
	}
	if _, err := Existing(now); err != nil {
		t.Fatalf("result must stay valid TOML: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/keys/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write `internal/keys/keys.go`**

```go
// Package keys implements `herdr-dots setup-keys`: it proposes keybindings for
// the plugin's actions, refuses to override existing or built-in keys, and
// appends the block to the user's Herdr config after confirmation.
package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Binding struct {
	Key         string
	Action      string
	Description string
}

func Defaults() []Binding {
	return []Binding{
		{Key: "prefix+d", Action: "herdr-dots.manage", Description: "herdr-dots: manage dots"},
		{Key: "prefix+a", Action: "herdr-dots.toggle-pane", Description: "herdr-dots: toggle Dots pane"},
		{Key: "prefix+shift+s", Action: "herdr-dots.summon", Description: "herdr-dots: summon a dot"},
	}
}

// BuiltinPrefixKeys are Herdr 0.8.2's default prefix bindings
// (`herdr --default-config`), treated as taken.
var BuiltinPrefixKeys = func() []string {
	ks := []string{
		"prefix+?", "prefix+b", "prefix+c", "prefix+e", "prefix+g", "prefix+h", "prefix+j", "prefix+k",
		"prefix+l", "prefix+minus", "prefix+n", "prefix+o", "prefix+p", "prefix+q", "prefix+r", "prefix+s",
		"prefix+v", "prefix+w", "prefix+x", "prefix+z", "prefix+tab", "prefix+shift+tab", "prefix+alt+g",
		"prefix+shift+d", "prefix+shift+g", "prefix+shift+n", "prefix+shift+p", "prefix+shift+r",
		"prefix+shift+t", "prefix+shift+w", "prefix+shift+x",
	}
	for i := 1; i <= 9; i++ {
		ks = append(ks, fmt.Sprintf("prefix+%d", i), fmt.Sprintf("prefix+shift+%d", i), fmt.Sprintf("prefix+alt+%d", i))
	}
	return ks
}()

func ConfigPath(getenv func(string) string, home string) string {
	if p := getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "herdr", "config.toml")
	}
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// Existing maps every bound key in the user's config to what it triggers.
func Existing(data []byte) (map[string]string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	keysTable, _ := raw["keys"].(map[string]any)
	for name, v := range keysTable {
		switch val := v.(type) {
		case string:
			if name != "prefix" && val != "" {
				out[val] = "herdr: " + name
			}
		case []map[string]any:
			for _, c := range val {
				k, _ := c["key"].(string)
				cmd, _ := c["command"].(string)
				if k != "" {
					out[k] = cmd
				}
			}
		}
	}
	return out, nil
}

type Conflict struct {
	Binding Binding
	With    string
}

func Plan(want []Binding, existing map[string]string) (add, already []Binding, conflicts []Conflict) {
	builtin := map[string]bool{}
	for _, k := range BuiltinPrefixKeys {
		builtin[k] = true
	}
	for _, b := range want {
		if cur, ok := existing[b.Key]; ok {
			if cur == b.Action {
				already = append(already, b)
			} else {
				conflicts = append(conflicts, Conflict{Binding: b, With: cur})
			}
			continue
		}
		if builtin[b.Key] {
			conflicts = append(conflicts, Conflict{Binding: b, With: "herdr built-in"})
			continue
		}
		add = append(add, b)
	}
	return add, already, conflicts
}

func Block(bs []Binding) string {
	var b strings.Builder
	b.WriteString("\n# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n")
	for _, k := range bs {
		fmt.Fprintf(&b, "\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n", k.Key, k.Action, k.Description)
	}
	return b.String()
}

// Append backs up the config next to itself, then appends the block.
func Append(path string, bs []Binding, now time.Time) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	backup := path + ".bak-" + now.Format("20060102-150405")
	if err := os.WriteFile(backup, orig, 0o600); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return backup, err
	}
	defer f.Close()
	_, err = f.WriteString(Block(bs))
	return backup, err
}
```

- [ ] **Step 4: Write `cmd/herdr-dots/setupkeys.go`**

```go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"herdr-dots/internal/keys"
)

func runSetupKeys(args []string) error {
	fs := flag.NewFlagSet("setup-keys", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "append without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	path := keys.ConfigPath(os.Getenv, home)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing, err := keys.Existing(data)
	if err != nil {
		return fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	add, already, conflicts := keys.Plan(keys.Defaults(), existing)
	for _, b := range already {
		fmt.Printf("✓ %s already runs %s\n", b.Key, b.Action)
	}
	for _, c := range conflicts {
		fmt.Printf("✗ %s is taken by %s; skipping %s (bind it yourself to another key)\n", c.Binding.Key, c.With, c.Binding.Action)
	}
	if len(add) == 0 {
		fmt.Println("Nothing to add.")
		return nil
	}
	fmt.Printf("\nThis will be appended to %s:\n%s\n", path, keys.Block(add))
	if !*yes {
		fmt.Print("Append it? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
	}
	backup, err := keys.Append(path, add, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("Appended. Backup: %s\n", backup)
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	if out, err := exec.Command(bin, "server", "reload-config").CombinedOutput(); err != nil {
		fmt.Printf("Run `herdr server reload-config` yourself (%v: %s)\n", err, strings.TrimSpace(string(out)))
	} else {
		fmt.Println("Herdr config reloaded.")
	}
	return nil
}
```

In `main.go`: `"setup-keys": runSetupKeys,`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/keys/ && go build ./... && go vet ./...`
Expected: PASS. If `TestExistingReadsCommandsAndKeyTable` fails because BurntSushi decodes `[[keys.command]]` as `[]any` instead of `[]map[string]any`, add a `case []any:` branch that type-asserts each element to `map[string]any`.

- [ ] **Step 6: Commit**

```bash
git add internal/keys cmd/herdr-dots
git commit -m "feat: setup-keys helper with conflict checks, backup and reload"
```

---

### Task 22: README, end-to-end smoke test and scaffold review

**Files:**
- Create: `README.md`
- Modify: `docs/superpowers/notes/2026-10-08-verification.md` (append E2E results)

- [ ] **Step 1: Write `README.md`**

````markdown
# Herdr Dots

> **Status: scaffold (v0.1.0).** This is the first increment, the core loop. It is not finished
> and not ready for general use. See [Roadmap](#roadmap) for what's missing.

Herdr Dots is a [Herdr](https://herdr.dev) plugin that adds **dots**: specialist agents (security,
frontend, backend, copywriter, ...) that join a Claude Code or Codex session running in a Herdr
pane. After every turn of the main agent, each attached dot checks the turn against its purpose and
speaks up only when it has something worth saying.

## Requirements

- Herdr ≥ 0.8.0 on macOS or Linux
- Go ≥ 1.25 (to build)
- `claude` (Claude Code) logged in, for `claude-cli` dots; `codex` logged in, for `codex-cli` dots

## Install

```bash
git clone <this repo> && cd herdr-dots
make build
herdr plugin link .        # development install
```

The engine starts automatically with Herdr (startup hook), or the first time you use a Dots action.

## Keybindings

Run `./bin/herdr-dots setup-keys` from a shell inside Herdr. It shows what it will add, skips keys
that are already taken, backs up your config, and reloads Herdr. Or paste this into
`~/.config/herdr/config.toml` yourself and run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+d"
type = "plugin_action"
command = "herdr-dots.manage"
description = "herdr-dots: manage dots"

[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "herdr-dots.toggle-pane"
description = "herdr-dots: toggle Dots pane"

[[keys.command]]
key = "prefix+shift+s"
type = "plugin_action"
command = "herdr-dots.summon"
description = "herdr-dots: summon a dot"
```

All actions also work from Herdr's action menu without any binding.

## Using it

1. Focus a Claude Code or Codex pane and press `prefix+d`.
2. Press `t` to create a dot from a template (or `n` for a blank one), edit it, `ctrl+s` to save.
3. Check the dots you want with `space`, press `enter` to attach them.
4. Press `prefix+a` to open the Dots pane next to the agent.

Dots pane keys: `⏎` send card · `a` send all · `e` edit · `x` dismiss · `s` summon · `m` cycle mode ·
`p` pause · `d` detach · `r` reset an errored dot · `t` next agent · `f` feed · `?` help.

### Delivery modes

| Mode | Runs every turn? | Output |
|---|---|---|
| `observe` | no | Answers only when summoned |
| `suggest` | yes | Card you read and copy; toast for warnings |
| `queue` (default) | yes | Card you send with `⏎` |
| `inject` | yes | Sent automatically when the agent is idle and you're not in that pane |

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder

## Privacy and safety

- Dots send recent conversation turns to **their own** configured provider (Anthropic for
  `claude-cli`, OpenAI for `codex-cli`).
- Dots are read-only: claude dots get only `Read,Grep,Glob`; codex dots run with `--sandbox read-only`.
- Hooks are disabled for dot runs, and Herdr variables are stripped, so dots never affect your panes.
- Queue mode (you approve) is the default. Inject mode is opt-in per dot and can be turned off for
  all dots with `inject_enabled = false` in `config.toml`.
- Per-dot and per-session call budgets cap cost.

## Known gaps in this scaffold

- Codex transcript reading and the codex backend are built from Codex's documented formats and
  have **not been verified live**.
- The Dots pane opens at Herdr's default split width.
- No dot memory yet: dots don't learn a project between turns (see Phase 2).
- No council mode, no API/Ollama backends, no MCP server, no Windows support.

## Roadmap

| Phase | Increment |
|---|---|
| 1 | Core loop (this scaffold) |
| **2** | **Dot memory — VITAL.** Per-project, per-dot memory of conventions, decisions and past findings. Without it, dots repeat themselves and can't learn a project. This is the most important next increment. |
| 3 | Council mode: ask all dots at once, get one merged digest |
| 4 | API and Ollama backends behind the same backend interface |
| 5 | Dots MCP server so the main agent can consult dots itself |
| Later | Windows; marketplace publishing (reconsider the name, which overlaps with OpenAI's "Dots"); write-capable dots in sandboxed worktrees |
````

- [ ] **Step 2: Full automated check**

Run: `go test -race ./... && go vet ./... && make build && go test -tags live ./internal/backend/ -run LiveClaude -v`
Expected: all PASS; live claude test prints a verdict.

- [ ] **Step 3: End-to-end smoke test in the isolated session (agent part)**

```bash
herdr --session dots-test server   # background
herdr --session dots-test plugin link "$PWD"
herdr --session dots-test plugin list            # herdr-dots listed, enabled
herdr --session dots-test workspace create --cwd /tmp/dots-test-repo
herdr --session dots-test pane list               # note <pane_id>
herdr --session dots-test agent start dots-main --kind claude --pane <pane_id>
find ~/.config/herdr -path '*herdr-dots*' -name engine.log -newer go.mod   # engine log exists
```
Expected: `engine.log` contains `engine started`. Then:
`herdr --session dots-test plugin action invoke herdr-dots.toggle-pane` and `herdr --session dots-test pane list` → a new pane titled Dots next to `<pane_id>`. Record results in the verification notes. If the action can't find a focused pane in a headless session, record that and leave the toggle check to Step 4.

- [ ] **Step 4: End-to-end smoke test (human part)**

Ask the user to run, in Ghostty: `herdr --session dots-test`, then walk this checklist and report back:
1. `prefix+d` on the Claude pane → library opens; `t` → Security template → `ctrl+s` saves; `space`, `enter` attaches.
2. `prefix+a` → Dots pane opens beside the agent showing `◆ Security  queue  idle`.
3. Prompt Claude: "store the login token in localStorage in auth.ts". After the turn, a Security card appears (critical/warn), and Herdr's sidebar shows `claude ◆1`.
4. `⏎` on the card → the message appears in Claude as `[◆ Security dot] ...` and Claude responds; the Security dot does **not** review that reply (loop guard).
5. `m` to switch Security to `inject`; trigger another finding while focused on the Claude pane → card shows `[auto]`; move focus to the Dots pane → it gets delivered.
6. Close the Claude pane → the card shows `[orphaned]`.
7. `herdr --session dots-test server stop` when done.

Record each result (pass/fail + notes) in the verification notes file. Fix any failure through superpowers:systematic-debugging before continuing.

- [ ] **Step 5: Scaffold language review**

Run: `grep -rniE '\b(done|complete|finished|ready|production[- ]ready)\b' README.md herdr-plugin.toml cmd internal --include='*.go' --include='*.md' --include='*.toml'`
Expected: no claim that the project is done or ready (matches like "turn finished" or `doneMsg` in code are fine). Confirm the README roadmap line says **"Dot memory — VITAL"**.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/superpowers/notes/2026-10-08-verification.md
git commit -m "docs: README with scaffold status, Phase 2 dot memory marked VITAL; E2E smoke notes"
```

