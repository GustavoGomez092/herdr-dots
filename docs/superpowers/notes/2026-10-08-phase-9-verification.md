# Phase 9 (profiles, history and sharing) verification notes

- **Date:** 2026-10-09
- **Machine:** macOS (darwin/arm64). Windows and Linux are cross-compiled and unit-tested only.

## V1: history size and profile speed

- `BenchmarkStats30d` (one 20,000-line file read once for both windows and 50 rows): 35,631,350 ns/op (about 35.6 ms; planning run: 35 ms).
- `BenchmarkCostShareTenDots` (ten full files): 356,704,771 ns/op (about 357 ms; planning run: 370 ms), so the profile renders first and adds the cost hint when the share arrives.
- **Answer:** the 20,000-line cap (trimmed to 15,000) stays. Stats use rolling 7- and 30-day windows, not calendar days (P9-R2).

## V2: project dots outside git repositories

- **Answer:** no. Project identity is the git root commit; path identities (no git, no commits, shallow clones) get no project dots.

## V3: committed project memory

- **Answer:** out of scope. Recorded as a question for the user: should a repository be able to ship read-only project memory (for example `.herdr-dots/memory.toml`)?

## Behaviour documented in the README, checked against the code

- 14 templates, all with risky features off (`internal/dots/templates_test.go`).
- Export refuses secrets and names fields only; `--force` drops flagged memory entries; absolute paths are dropped.
- Import clamps risky capabilities (P9-R4: shared dots keep only `--model` and `--effort` args; budgets clamped); memory arrives as proposals labelled "imported" that never suppress repeats (P9-R8); a slug collision is refused; fetch is https only.
- Project dots: content-hash trust of the exact bytes; per-project trust (git root commit); "trusted at <path>" when trust came from another checkout (P9-R13); at most 64 files read per repository (`dots.MaxProjectFiles`).
- Control, bidi and invisible characters are refused in shared dots and imported memory (`internal/dots/hidden_test.go`, `internal/dotpack/hidden_test.go`).

## Live check (Task 11 Step 8)

- **Not run on this machine.** It is a user gate: in an isolated `herdr --session dots-test`, open the manage popup and try the profile (`i`), the import preview (`I`, read to the end, then confirm), and a project dot's trust (`T`), edit and pause. Snapshot and diff `~/.claude/settings.json` around it, and never type Claude slash commands in the panes.
- `~/.claude/settings.json` unchanged: not applicable (no live run).

## Phase 9 result

- Version 0.9.0. Not done: Phase 10 (hardening and release) remains, and the product is feature complete only after its acceptance checklist.
