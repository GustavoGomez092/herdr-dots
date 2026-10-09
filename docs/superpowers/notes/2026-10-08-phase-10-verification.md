# Phase 10 (release hardening) verification notes

Phase 10 ships `1.0.0-rc.1`. It builds everything up to a release and stops at
`docs/release/ready-to-publish.md`. Nothing here was pushed, tagged, listed or
published (release-gate amendments).

## V1–V4

| # | Question | Finding | Holds |
|---|---|---|---|
| V1 | Can a plugin find another plugin id's config/state dirs? | Not needed: the name stays "Herdr Dots" (decisions ruling 1); the migration framework (`internal/migrate`) makes a later rename one more migration | n/a |
| V2 | Is the name free (GitHub repo, marketplace, Homebrew/Scoop, trademark)? | Not checked: naming and publishing are the developer's decisions. "Herdr Dots" overlaps OpenAI's "Dots" (spec §2); listed in the ready-to-publish checklist | user gate |
| V3 | Can `[[build]]` reach the network during `herdr plugin install`? Where does Herdr install from? | herdr.dev/docs/plugins (2026-10-08): install clones with `git` (default branch, or `--ref`), shows a preview, runs build steps after confirmation, gives them no runtime env, and documents no network restriction. `plugin link` runs no build. There is no `plugin update`: reinstall | yes |
| V4 | Does install honor per-platform `[[build]]` items? | Yes: item-level `platforms` (Phase 6 decision 2, from Herdr's source) | yes |

## Tools on this machine

Read-only check (`command -v` and `--version` only; nothing installed):

| Tool | Result |
|---|---|
| goreleaser | not installed here |
| cosign | not installed here |
| govulncheck | not installed here |
| actionlint | /opt/homebrew/bin/actionlint |
| shellcheck | /opt/homebrew/bin/shellcheck |
| pwsh | not installed here |
| powershell | not installed here |
| codex | not installed here |
| herdr | ~/.local/bin/herdr (herdr 0.8.2) |
| claude | ~/.local/bin/claude |
| git | git version 2.52.0 |
| go | /opt/homebrew/bin/go (go1.25.7 darwin/arm64) |

GoReleaser is used only as `goreleaser release --snapshot --clean --skip=publish,sign`
when installed; otherwise `scripts/release-local.sh` builds the same archives.

## Baseline at the start of Phase 10

Base commit e9874e1. `GOTOOLCHAIN=local go test -race ./...`: every package `ok`
(packages without tests report `no test files`); `go vet ./...`: `vet-ok`.

```
ok  	herdr-dots/cmd/herdr-dots
ok  	herdr-dots/internal/backend
ok  	herdr-dots/internal/backend/all
ok  	herdr-dots/internal/backend/anthropicapi
ok  	herdr-dots/internal/backend/httpx
ok  	herdr-dots/internal/backend/ollama
ok  	herdr-dots/internal/backend/openaiapi
ok  	herdr-dots/internal/backend/openrouter
ok  	herdr-dots/internal/backend/toolloop
ok  	herdr-dots/internal/config
ok  	herdr-dots/internal/council
ok  	herdr-dots/internal/credentials
ok  	herdr-dots/internal/delivery
ok  	herdr-dots/internal/dotpack
ok  	herdr-dots/internal/dots
ok  	herdr-dots/internal/engine
ok  	herdr-dots/internal/fsutil
ok  	herdr-dots/internal/herdr
ok  	herdr-dots/internal/history
ok  	herdr-dots/internal/ipc
ok  	herdr-dots/internal/keys
ok  	herdr-dots/internal/lockfile
ok  	herdr-dots/internal/mcpinstall
ok  	herdr-dots/internal/mcpserver
ok  	herdr-dots/internal/memory
ok  	herdr-dots/internal/patchstore
ok  	herdr-dots/internal/paths
ok  	herdr-dots/internal/platform
ok  	herdr-dots/internal/pricing
ok  	herdr-dots/internal/projectdots
ok  	herdr-dots/internal/queue
ok  	herdr-dots/internal/schedule
ok  	herdr-dots/internal/secrets
ok  	herdr-dots/internal/state
ok  	herdr-dots/internal/textutil
ok  	herdr-dots/internal/transcript
ok  	herdr-dots/internal/ui/diff
ok  	herdr-dots/internal/ui/manage
ok  	herdr-dots/internal/ui/memui
ok  	herdr-dots/internal/ui/pane
ok  	herdr-dots/internal/ui/profile
ok  	herdr-dots/internal/ui/style
ok  	herdr-dots/internal/ui/summon
ok  	herdr-dots/internal/view
ok  	herdr-dots/internal/worktree
vet-ok
internal/version/version.go:5:const Version = "0.9.0"
herdr-plugin.toml:4:version = "0.9.0"
```

## User gates

Only the developer can do these; Phase 10 prepares each one.

1. Choose a license and add `LICENSE` (ruling 16). No license is chosen.
2. Decide the name for publishing (keep "Herdr Dots" or rename; the migration framework supports a rename).
3. Run the feature-complete acceptance run on Linux and on Windows (Herdr Windows beta) hardware, and on macOS with a real Herdr install.
4. Run the live provider checks: the Claude live tests and the provider part of the macOS smoke, the API backends with your keys, then install Codex and run the Codex live tests (`go test -tags live ./internal/backend/ ./internal/council/`).
5. Create the GitHub repository, push, and let CI run on three OSes.
6. Tag `v1.0.0-rc.N`, merge the checksum PR, set `internal/version/VERSION` to `1.0.0` when the acceptance run passes, tag `v1.0.0`, publish the draft release.
7. Add the `herdr-plugin` topic and check the listing on herdr.dev/plugins.
8. Install from the marketplace without Go on all six targets.

## Phase 10 wrap-up

(Task 13)
