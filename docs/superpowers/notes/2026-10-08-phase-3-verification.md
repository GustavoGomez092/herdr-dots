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
