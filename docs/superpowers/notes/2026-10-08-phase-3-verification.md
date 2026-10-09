# Phase 3 verification findings (V1–V4), 2026-10-08

Environment: herdr 0.8.2, Claude Code 2.1.295, macOS 26.5.2 (Darwin 25.5.0).

Every section records the command, the evidence and "Assumption holds: yes/no".

## Confirmed while planning
- V1: 10 of 10 synthetic councils produced a verified digest with `claude -p --model sonnet` (Claude Code 2.1.295) once the chair prompt quoted its headers exactly.
- V2: `herdr --default-config` (0.8.2) binds `prefix+c` (new tab), not `prefix+shift+c`.
- Codex is not installed: V3 covers Claude Code only.

## V1: chair reliability
Command: `RECORD=1 go test -tags live ./internal/council/ -run LiveChair -v -timeout 40m`
Model: sonnet (`claude -p`, the `claude-cli` backend).
Evidence: `V1 summary: 10 verified, 0 unverified, 0 malformed of 10`. All ten replies were read by eye: every one has `## ` headers, every Must fix / Should fix / Consider bullet ends in `(Name, …)`, failed members appear under "## Not heard from", and the round-2 council (07) put the objection under "## Disagreements". No label needed a rename and `ParseDigest` needed no change. Replies are in `internal/council/testdata/chair/verified-01.txt` … `verified-10.txt` (the `result` text, a JSON verdict).
Assumption holds: yes

## V2: the default key
Commands: `herdr --default-config | grep -nE 'prefix\+shift\+c'` and `grep -nE 'prefix\+shift\+c' ~/.config/herdr/config.toml`.
Evidence: both print nothing (exit 1). `prefix+shift+c` is free in the Herdr defaults and in the user's config.
Assumption holds: yes

## V3: a long council delivery into Claude Code
Setup: isolated session `dots-test`, server started with every `CLAUDE*` variable unset, workspace in `/tmp/dots-test-repo`, Claude started with `herdr --session dots-test agent start v3agent --kind claude --pane w4:p1 -- --model haiku` (equivalent to `claude --model haiku`). One short prompt first, then the council message through `herdr agent prompt` (the same call delivery uses).
Message: 3,206 characters (`wc -m`; 30 bullets, because the brief's 24 bullets only reach 2,576), starting `[⊕ Council dot] ## Must fix` and ending `## Not heard from\n- Copy (timed out)`.
Evidence from the transcript `~/.claude/projects/-private-tmp-dots-test-repo/ac7d2bca-….jsonl` (read only):
- The message arrived as exactly one user record (string content, 3,263 characters) and Claude answered once (one turn, "Churned for 8s").
- All 30 bullets and the final "- Copy (timed out)" line are in the record; nothing was truncated or split.
- `grep -c '^\[⊕ Council dot\]'` over the record text is 1.
- Difference from the brief: Claude Code wraps a long multi-line submission in paste tags, so the record text is `"\n\n<pasted_content id=\"5869\">\n[⊕ Council dot] ## Must fix\n…- Copy (timed out)\n</pasted_content id=\"5869\">\n"`. It does not literally start with `[⊕ Council dot]`; the marker is on the second line of the record.
- This does not break the loop guard: `transcript.FromDots` uses the multiline regex `(?m)^\[\S+ (.+?) dot\]`. Run on the pasted record text it returns `[Council]`, so `Turn.IsFromDot` is true for every dot. (Checked with a throwaway test, removed.) Anything that matches the marker with a non-multiline anchor (`^` without `(?m)`, `HasPrefix`) would miss it.
- `herdr agent prompt … --wait --until idle` hung on the first short prompt (it waited for a state change that never came after a 1 s reply); the later call with `--wait --timeout 90000` returned normally.
Codex: not installed; documented-format only.
Assumption holds: yes (with the paste-wrapper caveat above)

## V4: wall-clock time of a five-member council
Command: `go test -tags live ./internal/council/ -run LiveCouncilWallClock -v -timeout 20m`
Evidence: `V4 summary: members 7s, chair 5s (err=<nil>), total 12s` (members 3–4 s each at 3 parallel).
That is far under `2 × run_timeout_seconds` (240 s by default). Informational, no gate; `max_parallel_runs` stays (spec O5 (a)).

## E2E (Task 8)
Setup: isolated session `dots-test` (server started with every `CLAUDE*` variable unset; the session restored older workspaces, ignored), fresh workspace on `/tmp/dots-test-repo`, `claude --model haiku` started in the repo pane by sending the command text to the pane, prompt "store the login token in localStorage in auth.ts" through `herdr agent prompt --wait --timeout`. Engine run by hand with a temporary config/state dir and three haiku dots (`security`, `frontend` in queue mode, `copy` in observe mode; `turns = 3`, `repo_read = false`). Driven with a throwaway IPC client (not committed). Version 0.3.0. Visual pane layout was not checked by eye (not automatable here); the checks below use the snapshot JSON, the card and the feed.

1. Preflight: PASS. `members: [security, frontend, copy]` (observe dot included), `rounds: 1`, `chair: true`, `estimated_calls: 4`, `allowed: true`, `pane_left: 120`.
2. Progress and card: PASS, with a gap. `show` reported `"council": {"phase": "members", "done": 1, "total": 3, "round": 1}`; the `merging` phase was too short to catch by polling. Afterwards `council` was null and one card existed with `"kind": "council"`, `"mode": "queue"`, `"status": "pending"`, `"severity": "critical"`, three parts (security warn, frontend critical, copy with no body = passed), and a chair digest with `## Must fix` and `## Should fix` sections and attributed bullets `(Frontend)`, `(Security)`. No `note` field, so the chair merge was used.
3. Calls and feed: PASS. Pane calls went from 2 (two ordinary turn reviews that ran when the dots attached) to 6 (+4: 3 members and the chair). The feed has a `council` line: "3 of 3 members answered; critical".
4. Send and loop guard: PASS. After `send`, the Claude transcript has exactly one user record, `"\n\n<pasted_content id=\"…\">\n[⊕ Council dot] ## Must fix…"` (paste wrapper, as in V3), and Claude answered. For 80 s after the turn, calls stayed at 6 and the feed had no new lines, so no dot reviewed the council-started turn.
5. Second council while one runs: PASS. `council-e2e: council already running`.
6. Pane closed mid-council: PASS. After `pane close`, `engine.log` said `pane w5:p1 closed: 2 card(s) orphaned`, `council` was null, pane calls were reset to 0 and no council card appeared. Concern: the cancelled member runs were logged to the feed as `error` lines with detail "claude timed out: context canceled" (one per dot), although the pane was already gone; cosmetic wording for a cancelled run.

Cleanup: engine killed, `dots-test` server stopped, throwaway driver and temp dirs removed, the global Claude settings file unchanged (diff against a snapshot taken before the run was empty).
