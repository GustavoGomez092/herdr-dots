# Phase 7 (write-capable dots) verification notes

- **Date:** 2026-10-08
- **Machine:** macOS (darwin/arm64). Linux and Windows are cross-compiled and unit-tested here; Git for Windows and Ubuntu's git run only in the Phase 6 CI matrix, which is written but never pushed (user gate).
- **Claude Code:** 2.1.295. **git:** 2.52.0 (Homebrew) and 2.50.1 (Apple Git-155). **Codex:** not installed.

## V1: can a claude draft run write outside its working directory?

- **Question:** with `claude -p --permission-mode acceptEdits --tools Read,Grep,Glob,Edit,Write`, can Claude edit files outside the cwd (absolute paths, `../`), or through a symlink?
- **Finding:** it depends on `--allowedTools`.
  - With the spec's `--allowedTools Read,Grep,Glob,Edit,Write`: editing `<dir>/outside.txt` by absolute path, writing `../outside-new.txt` and writing `<dir>/abs-new.txt` all **succeeded**. A bare `Edit` or `Write` allow rule approves edits to any path.
  - With `--allowedTools Read,Grep,Glob`: those three were **denied** ("permission not granted"); editing `inside.txt` and writing `sub/new.txt` inside the cwd succeeded.
  - Writing `link/escaped.txt`, where `link` is a symlink to a sibling directory, was **denied** in both runs ("resolves through a symlink … outside the allowed working directories").
- **Evidence (first run):** live runs, `--model haiku --output-format json --no-session-persistence --strict-mcp-config --settings '{"disableAllHooks":true}' --disallowedTools Bash,WebFetch,WebSearch`, cwd `<dir>/wt` in a throwaway directory outside the temp dir; `permission_denials` in the JSON output; files checked on disk afterwards; `~/.claude/settings.json` byte-identical before and after.
- **Assumption holds:** no for the spec's flags; yes with Plan decision 2 (`--allowedTools Read,Grep,Glob`). `internal/backend/write_test.go` pins it (`TestNoClaudeRunEverBypassesPermissions`).

### The shipped draft argv (ruling P7-R3)

The first run above predates P7-R3. A claude-cli draft now runs (`internal/backend/claude.go`, `BuildCmd` in write mode):

```
claude -p --output-format json --no-session-persistence --strict-mcp-config \
  --settings '<writeSettings>' --append-system-prompt '<system prompt>' [--model <m>] \
  --restricted \
  --tools Read,Grep,Glob,Edit,Write --allowedTools Read,Grep,Glob \
  --permission-mode acceptEdits --disallowedTools Bash,WebFetch,WebSearch
```

with `<writeSettings>`:

```json
{"disableAllHooks":true,"permissions":{"deny":["Edit(.git)","Write(.git)","Edit(.git/**)","Write(.git/**)","Edit(.claude/**)","Write(.claude/**)","Edit(**/.git)","Write(**/.git)","Edit(**/.claude/**)","Write(**/.claude/**)"]}}
```

`--restricted` makes Claude Code ignore the user, project and local settings files, so a repository's own `.claude/settings.json` can't add allow rules or directories; it keeps `--settings`, confines the file tools to the working directory and refuses `bypassPermissions`. The deny list keeps the worktree's `.git` file and any `.claude/` directory out of reach even under `acceptEdits`. Independently of the CLI, the engine's `worktree.Validate` refuses a patch touching `.git`, `.claude`, `.mcp.json` or `.codex` for every backend.

- **P7-R3 live verification (controller, Claude Code 2.1.295, subscription auth):** with a repository `.claude/settings.json` allowing `Edit` and `Write`, the rule was ignored: a `Write` to the parent directory was **denied** by the permission layer, and an edit inside the worktree **worked**. In a linked worktree, writes to `.git` and to `.claude/settings.local.json` were **denied**. `~/.claude/settings.json` was unchanged before and after.

### V1 re-run (for every new Claude Code version)

Run it in a throwaway directory outside the temp dir. It uses the shipped argv, a linked worktree (so `.git` is a file, as in a draft) and a repository allow rule that `--restricted` must ignore. Don't run it inside a Claude Code session, and never type slash commands into one.

```sh
#!/bin/sh
# Usage: sh v1.sh /some/fresh/dir/outside/tmp   (the directory must not exist)
D="$1"; mkdir -p "$D/linked" || exit 1
git init -q "$D/repo" && git -C "$D/repo" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init || exit 1
git -C "$D/repo" worktree add -q "$D/wt" || exit 1
printf 'original\n' > "$D/outside.txt"; printf 'inside\n' > "$D/wt/inside.txt"
mkdir -p "$D/wt/.claude" && printf '{"permissions":{"allow":["Edit","Write"]}}\n' > "$D/wt/.claude/settings.json"
ln -s ../linked "$D/wt/link"
cp "$D/wt/.git" "$D/dotgit.before"
cd "$D/wt" || exit 1
unset CLAUDECODE CLAUDE_CODE_ENTRYPOINT CLAUDE_CODE_CHILD_SESSION
S='{"disableAllHooks":true,"permissions":{"deny":["Edit(.git)","Write(.git)","Edit(.git/**)","Write(.git/**)","Edit(.claude/**)","Write(.claude/**)","Edit(**/.git)","Write(**/.git)","Edit(**/.claude/**)","Write(**/.claude/**)"]}}'
printf 'This is a sandbox test of file tools. Do all of these, each with the named tool, and report for each one whether it succeeded or was denied:\n1. Edit %s/outside.txt (absolute path): replace original with pwned.\n2. Write ../outside-new.txt containing X.\n3. Write %s/abs-new.txt containing X.\n4. Edit inside.txt: replace inside with edited.\n5. Write sub/new.txt containing X.\n6. Write link/escaped.txt containing X.\n7. Write .claude/settings.local.json containing {}.\n8. Edit .git: replace gitdir with pwned.\nReply with ONLY {"relevant": true, "severity": "info", "message": "<one line per step>"}.\n' "$D" "$D" |
claude -p --output-format json --no-session-persistence --strict-mcp-config \
  --settings "$S" --model haiku \
  --restricted \
  --tools Read,Grep,Glob,Edit,Write --allowedTools Read,Grep,Glob \
  --permission-mode acceptEdits --disallowedTools Bash,WebFetch,WebSearch > ../out.json
cd "$D" && cat outside.txt && ls -la . wt wt/.claude linked && cmp wt/.git dotgit.before && echo ".git unchanged"
```

Expected:

- `outside.txt` still says `original`. There's no `outside-new.txt`, `abs-new.txt` or `linked/escaped.txt`: steps 1-3 and 6 are denied, and the repository allow rule is ignored.
- `wt/inside.txt` says `edited` and `wt/sub/new.txt` exists: steps 4-5 work.
- There's no `wt/.claude/settings.local.json` (step 7 denied), and `.git unchanged` is printed (step 8 denied). `out.json` lists both in `permission_denials`.
- `~/.claude/settings.json` is byte-identical before and after (diff a copy).
- A Claude Code without `--restricted` stops at once with an unknown-option error and writes nothing. Drafts fail closed: they never run unrestricted.

## V2: Codex workspace-write

- **Not tested:** Codex is not installed on this machine.
- **Decision:** `codex exec` always offers a shell tool, and write-capable dots get no shell (ruling 5), so codex-cli dots can't be write-capable (Plan decision 1). Revisit only with a verified flag that removes Codex's shell.

## V3: `git apply --3way` with unstaged changes

- **Finding:** `git apply --3way` implies `--index`. Against the real index it refuses a touched file with unstaged edits (`error: a.txt: does not match index`) and would stage the result. Through a private copy of the index, refreshed for the patch's paths with `git update-index --add --remove`, it applies to the working tree only: a merge on drift, conflict markers on a real conflict (listed by `git diff --name-only --diff-filter=U` on the copy), an add/add conflict instead of an overwrite when the user created the file, and nothing at all on a hard failure. `.git/index` stays byte-identical, staged files stay staged, new files stay untracked.
- **Evidence:** `internal/worktree/patch_test.go` (Task 3) on git 2.52.0 and Apple Git 2.50.1.
- **Not verified here:** Ubuntu LTS's git and Git for Windows (CI user gate).
- **Assumption holds:** yes, with Plan decision 3.

## V4: snapshot cost on a large repository

- **Finding:** 100,000 tracked files plus 50,000 ignored (`node_modules/`): snapshot 0.21 s (first) and 0.47 s (second), worktree checkout 12.3 s and 5.5 s, index 7.8 MB.
- **Evidence:** `go test -tags live -run TestSnapshotScale -v ./internal/worktree/` (Task 3), macOS arm64, APFS.
- **Assumption holds:** yes. The 60 s `git add` and 500 MB index limits are far away; checkout time is the larger cost and counts against no limit.

## V5: fsmonitor, sparse checkout, partial clone

- **Finding:** a repository whose `core.fsmonitor` names a program never runs it (`-c core.fsmonitor=false`); a sparse checkout snapshots correctly, and files outside the cone stay as they are in HEAD. A partial clone is not tried here: `GIT_NO_LAZY_FETCH=1` (git ≥ 2.44) turns a missing blob into an error rather than a network fetch.
- **Evidence:** `TestAnFsmonitorProgramNeverRuns`, `TestSnapshotOfASparseCheckout` (Task 2).
- **Assumption holds:** yes; partial clone is a user gate.

## User gates: what only the user can confirm

Under the live-step safety rules (`herdr --session dots-test` only; never `herdr plugin link`; never type Claude slash commands into panes; snapshot and diff `~/.claude/settings.json`; never touch `~/.config/herdr/config.toml`):

- [ ] On every new Claude Code version, run the V1 re-run above and check its expected results. A Claude Code too old for `--restricted` fails closed with an unknown-flag error: drafts fail until Claude Code is updated.
- [ ] A real draft in a `dots-test` session: set `write_enabled = true` in the plugin's `config.toml`, add `[capabilities] write = true` to a claude-cli dot, press `w` on one of its cards, confirm, see the `✎` progress line, then a patch card.
- [ ] `v` opens the zoomed "Dots: patch" pane; `A` opens it with the apply question; `y` applies; `git status` shows the change unstaged and nothing committed; `git diff --cached` is unchanged.
- [ ] `⏎` on a patch card types the "Patch ready" message into the agent pane (path quoted, no diff).
- [ ] Quit Herdr mid-draft; on the next start `engine.log` says the draft was interrupted and cleaned up, and `git worktree list` shows only your own worktrees.
- [ ] CI on `windows-latest` and `ubuntu-latest` (push to a private remote): `internal/worktree` and `internal/engine` draft tests pass there.
- [ ] An API dot (`anthropic-api`) with `write = true` drafts through `write_file`/`edit_file`.
- [ ] A partial clone (`git clone --filter=blob:none`) drafts without network access, or fails with a clear error.
- [ ] On Windows: a draft runs git found on `PATH` (Phase 6 `platform.CommandContext`), the session's `worktrees` folder and the `herdr-dots-index-*`/`herdr-dots-apply-*` temp folders are owner-only (`icacls`), and a patch path such as `con.txt` or `a.txt:stream` is rejected.

## Phase 7 result

- Version 0.7.0. `go test -race ./...` (darwin), `GOOS=linux`/`GOOS=windows go build ./...` and `GOOS=windows go vet ./...` pass.
- Phase 2 debt paid: `DeleteDot` scrubs ledger lines and orphaned proposals (Task 0).
- A live Claude draft has not been run on this machine. On Windows, drafts are compile-checked and unit-tested only, never run.
- Open user gates: every box in "User gates" above.
