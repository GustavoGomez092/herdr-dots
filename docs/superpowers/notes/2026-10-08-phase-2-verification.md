# Phase 2 verification findings (V1–V6), 2026-10-08

Environment: herdr 0.8.2, claude 2.1.295 (Claude Code), git 2.52.0, macOS 26.5.2 (Darwin 25.5.0, arm64). Codex is not installed. jq present.

Every section records the command, the evidence and "Assumption holds: yes/no".

## Confirmed while planning
- `herdr plugin config-dir herdr-dots` prints the plugin config dir (Herdr 0.8.2). No `state-dir` command exists, so `paths.Discover` reads `CONFIG_DIR/.state-dir`, written by `Paths.Ensure()`.
- Codex is not installed: V3 covers `claude -p` only. Codex stays "documented-format, not live-verified" (BLOCKED: every codex item is unverified).

## V1: root commit of shallow clones
Command: the brief's script, run in a `mktemp -d` scratch directory (3-commit repo, `GIT_CONFIG_GLOBAL=/dev/null`).

```
full:      31856f32595bbaa7712e0a8aeda673b1d707945e
depth 1:   90fec75697f4bb5b913fbbf27a6301e31e0b1dd7  shallow=true
depth 2:   3e16e1ea3d6924e870d258e6ddfd2825d9ee2244  shallow=true
clone:     31856f32595bbaa7712e0a8aeda673b1d707945e  shallow=false
worktree:  31856f32595bbaa7712e0a8aeda673b1d707945e
fatal: ambiguous argument 'HEAD': unknown revision or path not in the working tree.
empty repo exit=128
/dev/null
```
Full clone and worktree agree with the source; both shallow clones report different hashes (the shallow boundary) and `--is-shallow-repository = true`; the empty repo exits non-zero; `core.hooksPath` prints `/dev/null`.
Assumption holds: yes

## V2: config dir outside Herdr
Command: `herdr plugin --help | grep -i -E 'config-dir|state' ; herdr plugin config-dir herdr-dots`
```
  config-dir  Print a plugin config directory
~/.config/herdr/plugins/config/herdr-dots
```
A `config-dir` line, no `state-dir` line, exactly one path printed.
Assumption holds: yes

## V3: does `claude -p` follow the optional `remember` field?
Command: the brief's loop (10 scenarios x 2 rounds = 20 review replies, plus 4 memory-pass replies), `claude -p --output-format json --no-session-persistence --strict-mcp-config --settings '{"disableAllHooks":true}' --tools "" --model sonnet --append-system-prompt "$(cat sys.txt)"`, with `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT` and `CLAUDE_CODE_CHILD_SESSION` unset. Replies are in `internal/memory/testdata/replies/`.

Results:
- Review replies parse: 20/20 are valid JSON when checked directly (`jq -e . file`). The brief's measuring snippet flags `review-03.txt` and `review-13.txt` as MALFORMED, but that is an artifact of the snippet: it pipes through zsh `echo`, which interprets the `\\` and `\"` escapes inside those two messages. Both files are valid JSON. By the snippet as written: 2/20 flagged (limit 1); by real parsing: 0/20. No reply used code fences.
- Memory-pass replies: 4/4 valid, shape `{"remember":[...]}`.
- Ops per review reply: 15 replies carry 0 ops, 5 carry 1 op (review-04, 09, 13, 14, 20), none more than 1. Pass replies carry 1, 1, 2 and 3 ops.
- Scenarios: 4 (zod rule) proposed a project convention in 2/2 rounds (review-04, review-14); 9 (fly.io secrets) proposed a project decision in 1/2 (review-09); 2 (httpOnly cookies, review-02 and 12) and 5 (skip CSRF, review-05 and 15) proposed nothing in review mode, so the brief's expectation that 2, 4, 5 and 9 propose conventions is only partly met in review mode. In the memory pass, scenarios 2, 4, 5 and 9 all produced ops (pass-01..04), so the pass path covers them. Scenario 8 in review mode (the rename) returned `relevant:false` twice (review-08, 18; 55 characters).
- Size: review replies average 696 characters (13,928 / 20); max 980. Under the 1,500 limit. Op texts in the pass replies are 102–284 characters (cap is 500).
- Secrets/personal-path grep (credential prefixes, e-mail addresses, home-directory paths): the only hits are case-insensitive matches of the home-path pattern on the scenario's own `internal/users/search.go` in review-03 and review-13 (false positives). No credentials, e-mail addresses or real home-directory paths in any reply file.
- `usage.input_tokens` is 2 on these calls because the prompt is served almost entirely from the prompt cache (see V4).

Apply results (Task 7, `TestRecordedRepliesParseAndApply`): all 24 replies parse (`ParseReply` for review-*, `ParseMemoryOps` for pass-*) and their ops apply to a fresh store as dot `security`: 10 ops in total (pass-01: 1, pass-02: 1, pass-03: 2, pass-04: 3, review-03, 04, 09, 13, 14, 20: 1 each), 10 applied, 0 rejected.

Assumption holds: yes (0/20 genuinely malformed; the snippet over-counts 2 because of `echo` escape handling; replies average 696 characters).

## V4: cost of a 6,000-character memory block
Command: the brief's loop (3 runs x plain/memory), `mem.txt` = 5,957 characters. `usage.input_tokens` is 2 in every run because of prompt caching, so the real input size is `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`.

| run | variant | total input tokens | cost USD | wall seconds (API ms) |
|-----|---------|--------------------|----------|-----------------------|
| 1 | plain | 3,728 | 0.0039 | 5 (3,442) |
| 1 | memory | 5,907 | 0.0152 (one-time cache write) | 5 (3,027) |
| 2 | plain | 3,728 | 0.0032 | 4 (2,872) |
| 2 | memory | 5,907 | 0.0042 | 5 (3,575) |
| 3 | plain | 3,728 | 0.0046 | 5 (3,721) |
| 3 | memory | 5,907 | 0.0038 | 4 (3,289) |

The memory block adds about 2,180 input tokens (slightly above the brief's 1,200–2,000 estimate). Latency difference is within noise (API time 3.0–3.6 s memory vs 2.9–3.7 s plain). After the first cached call the cost matches plain; the first memory run paid a one-time cache write.
Assumption holds: yes (latency impact well under 3 s; token delta about 2.2k, marginally above the 2,000 estimate, so keep the `prompt_chars = 6000` default and do not raise it).

## V5: flock on the state directory's filesystem
The brief's procedure (start `herdr --session dots-test server`, `plugin link`, read `HERDR_PLUGIN_STATE_DIR` from the engine process) was NOT run as written. Why the procedure changed:
- The brief's `plugin link` step would have changed the user's global plugin registry (`~/.config/herdr/plugins.json`). That registry is shared by all sessions, not per `--session`, so `--session dots-test` does not isolate it. The task's safety rules forbid touching `~/.config/herdr`, so read-only evidence was used instead.
- `herdr server` also has no start form in 0.8.2 (only `stop`, `reload-config` and the agent-manifest commands); a session server starts when a client attaches.

What the read-only evidence shows: Herdr had already created the herdr-dots plugin dirs earlier (from prior probing, not by this task). The state directory `~/.local/state/herdr/plugins/herdr-dots` exists, sits on a local APFS volume (`apfs, local`), and is not under a synced folder. It does not show what `HERDR_PLUGIN_STATE_DIR` is for a running engine; that is inferred from the on-disk layout, which is `~/.config/herdr/plugins/config/<id>` for config and `~/.local/state/herdr/plugins/<id>` for state (the herdr-plus plugin has both):
```
$ ls -ld ~/.local/state/herdr/plugins/herdr-dots
drwxr-xr-x@ 2 <user> <group> 64 Oct 8 19:54 ...
$ df -P ~/.local/state/herdr/plugins/herdr-dots
/dev/disk3s5  965595304 660642224 253988280  73%  <data volume>
$ mount | grep Data
/dev/disk3s5 on <data volume> (apfs, local, journaled, nobrowse, protect, root data)
```
The path is under `~/.local/state`, not Desktop/Documents, iCloud, Dropbox or OneDrive: not a synced folder. APFS local supports flock. The exact value of `HERDR_PLUGIN_STATE_DIR` was not read from a live engine process, so the path is inferred from the on-disk layout. No Herdr server was started by this task; nothing to stop.
Assumption holds: yes (inferred, not read from a live engine process).

## V6: labeled pairs for the 0.7 suppression threshold
`internal/memory/testdata/similar_pairs.tsv`: header `label<TAB>message<TAB>remembered`, 40 rows (20 `same`, 20 `different`): the brief's 30 verbatim plus 5 `same` and 5 `different` rows built from real V3 replies (review-01, 06, 07, 10, 03). `awk -F'\t'` confirms 3 fields on every row. Task 7's `TestSuppressionThresholdOnLabeledPairs` is the actual check (fails if more than 10% of `different` rows reach 0.7). Result at threshold 0.7: recall 17/20, false positives 0/20.
Assumption holds: yes (0/20 false positives, under the 10% limit).

## Safety
`~/.claude/settings.json` was snapshotted to `/tmp/claude-settings-before.json` before the first live step; `diff` after the last live step printed nothing.
