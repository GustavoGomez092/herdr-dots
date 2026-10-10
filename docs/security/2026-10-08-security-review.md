# Security review, 2026-10-08 (1.0.0-rc.1)

Method: the Claude Code `security-review` skill over the whole tree, a human
read of the files in Task 11 Step 3, and govulncheck.

The skill could not run as shipped: it starts from `git diff origin/HEAD...`,
and this local-only repository has no `origin` (nothing is pushed, by the
release-gate rules). Its method (high-confidence, exploitable findings in
injection, code execution, authentication and authorization, crypto, and
data exposure) was applied by hand to the whole tree at `263431b`, without
subagents. The read covered every file Step 3 names (`internal/backend/claude.go`,
`codex.go`, `codex_mcp.go`, `exec.go`, `internal/dots/args.go`, `clamp.go`,
`project.go`, `internal/backend/toolloop/*`, `internal/credentials/*`,
`internal/secrets/secrets.go`, `internal/memory/apply.go`, `importpack.go`,
`project.go`, `internal/engine/deliver.go`, `draft.go`, `projectdots.go`,
`internal/ipc/*`, `internal/mcpserver/*`, `internal/worktree/*`,
`internal/projectdots/*`, `internal/dotpack/*`, `internal/history/history.go`,
`internal/migrate/*`, `cmd/herdr-dots/uninstall.go`, `verifyrelease.go`,
`scripts/build.sh`, `build.ps1`, `.github/workflows/release.yml`) plus
`internal/paths`, `internal/delivery`, `internal/textutil`, the Anthropic
adapter's web tools and the MCP command. Findings were judged against the
current code, not the triage notes; where a finding matches an accepted
triage row, the row id is given.

govulncheck: `Your code is affected by 24 vulnerabilities from the Go
standard library` (go1.25.7, the toolchain on this machine); no
vulnerability in a third-party module is reachable (8 more in imported
packages and 10 in required modules are not called, all standard library).
`govulncheck@v1.1.4` could not fetch `vuln.go.dev` from its own resolver
(`lookup vuln.go.dev: i/o timeout`), so the database was downloaded with
curl (`https://vuln.go.dev/vulndb.zip`, 2026-10-09) and passed with `-db
file://`; `@latest` (v1.8.0) needs Go 1.26. See R2.

Severity: **Critical** means remote or unattended compromise in the default
setup; **High** means code execution, credential theft or an unapproved
change from untrusted input in a supported setup; **Medium** means a
weakness that needs a specific setup, a provider's behaviour, or a process
that already runs as the developer, or that only exposes data to the
developer's own agent; **Low** means denial of service, hardening or
defence in depth.

| # | Finding | Where | Severity | Disposition |
|---|---|---|---|---|
| R1 | claude-cli review, ask, memory, council and chair runs loaded the repository's `.claude/settings.json` and `.claude/settings.local.json`. `claude -p` skips the workspace trust dialog (its own `--help` says so), so a cloned repository could name commands Claude Code runs (`apiKeyHelper`, `awsAuthRefresh`, `awsCredentialExport`, `gcpAuthRefresh`), set `env` (`ANTHROPIC_BASE_URL` sends the prompt and the developer's credential to another host) or enable plugins, on every reviewed turn with no approval. Hooks were already off (`disableAllHooks` in `--settings`) and MCP servers out (`--strict-mcp-config`); drafts and research already ran `--restricted`. Exposed in practice when the pane's agent is not Claude Code (a Claude Code agent has accepted the folder's trust, so the same settings already apply to it) | `internal/backend/claude.go:113` (`BuildCmd`) | High | fixed in docs(security): threat model, security review and SECURITY.md (Phase 10): every claude-cli run without `--restricted` passes `--setting-sources user` before the dot's args (only the developer's user settings load; `--settings` still applies); a dot's args can't pass the flag in either form (already on the denylist; the inline form is pinned by `TestDotArgsCannotReloadRepositorySettingsInline`), nor `--managed-settings`, another settings tier (denied in the small round, `TestValidateRejectsUnsafeBackendArgs`). Tests: `TestClaudeRunsNeverLoadRepositorySettings`, `TestDotArgsCannotReloadRepositorySettings`; `TestShortPromptStaysInArgvExactlyAsV1` updated. The flag is in the installed Claude Code 2.1.295 (`--setting-sources <sources>`, read from the binary, not run); a live run is a user gate (with X8) |
| R2 | Release binaries would carry known standard-library vulnerabilities: the workflows install Go with `go-version-file: go.mod`, and `go.mod` says `go 1.25.0` with no `toolchain` line, so setup-go most likely installs go1.25.0 itself (under `GOTOOLCHAIN=local`), older than this machine's go1.25.7. govulncheck finds 24 reachable vulnerabilities already in go1.25.7 (`net/http` HTTP/2 client loop GO-2026-4918, `crypto/tls` KeyUpdate DoS GO-2026-4870, `crypto/x509` chain building, `archive/tar` sparse allocation GO-2026-4869 in `verify-release`, `net/url`, `os` GO-2026-4602); go1.25.0 has those and any fixed in 1.25.1–1.25.7. All are denial of service or metadata leaks reached through a configured provider, a pack URL or a release archive; none gives code execution | `go.mod:3`, `.github/workflows/ci.yml` and `release.yml` (every setup-go step) | Medium | known issue, user gate (triage X12): set an explicit patched `go-version` in every workflow (at least go1.26.9 on 2026-10-09, the newest patch then) instead of `go-version-file: go.mod`, and rerun `make vulncheck` until it reports `No vulnerabilities found.`; source builds use the developer's own Go. Enforced on a tag from this task on: the release workflow's `candidate` job runs govulncheck (as ci.yml's `vulncheck` job) before it builds or proposes checksums, so a vulnerable toolchain stops the candidate. `go.mod` and the setup-go versions are not changed here: Go 1.26 isn't installed and every command runs with `GOTOOLCHAIN=local`, so the change couldn't be tested |
| R3 | claude-cli review, ask, memory and council runs allow `Read,Grep,Glob` with no path rules: they can read outside the repository (`~/.ssh`, `~/.aws`, `credentials.toml`) and get no secret-file deny list, while API dots get the jail and `DefaultDeny` and claude-cli research gets `researchSettings`. A prompt-injected dot can quote a secret into a card: the developer sees it, and in inject mode it is typed into the agent unapproved. No network channel exists (no Bash, no WebFetch, no MCP); memory refuses it (scanner) and history redacts key shapes | `internal/backend/claude.go:65` (`hooklessSettings`) | Medium | partly fixed in the Task 11 small round: every claude-cli run with the read tools now gets `researchSettings(c.Deny)` (the secret-file Read deny rules plus `disableAllHooks`) instead of the bare hookless settings (`TestClaudeRepoReadRunsDenySecretFiles`). Follow-up: confine reads to the working directory, and live-verify Read deny against Grep and Glob (triage X9). The MCP `ask_dot` path runs such a dot too, and its answer reaches the agent without the queue (threat model B7). Listed in the threat model's known gaps |
| R4 | The engine endpoint is unauthenticated for processes of the same user, the coding agent included (through its shell): any of them can send, approve, trust a project dot, set inject mode, start a draft or apply a patch (the hash is in a snapshot). The MCP server's query-only design doesn't limit a process that dials the socket itself | `internal/ipc/ipc.go:169` (`Serve`), `internal/ipc/endpoint_unix.go` | Medium | known issue (triage I8, accepted): the 0700 directory and 0600 socket (Unix) and the user-only pipe with an owner check (Windows, P6-R2) keep other users out; within one user account there is no boundary (the same process could edit the files the engine reads). Stated plainly in the threat model (B9) and `SECURITY.md` |
| R5 | anthropic-api research declares the repository read tools together with server-side `web_fetch`. Anthropic fetches only URLs already in the context, and at most 5 web uses per run (P8-R6), so an injected page can leak a few bits by choosing which of its links to follow; search queries can carry repository text to the search provider (accepted in P8-R14) | `internal/backend/anthropicapi/anthropic.go:210` | Medium | follow-up: drop `web_fetch` when repository tools are offered, or restrict its domains; the provider's URL rule is not live-verified here (triage X11) |
| R6 | codex-cli dots run `codex exec` in the repository with no equivalent of `--setting-sources`: Codex reads the repository's `AGENTS.md` and, for a project the developer marked trusted in Codex, its `.codex/config.toml`; the plugin's deny globs don't limit what Codex reads | `internal/backend/codex.go:48` (`BuildCmd`) | Medium | known issue (triage S24, P1, X3): Codex isn't installed here and its paths are documented-format, not live-verified; follow-up when Codex is verified |
| R7 | Inject mode turns a dot's prompt-injected output into unapproved input for an agent that has a shell: repository text steers the dot, the dot's card is typed into the agent while the developer is away | `internal/engine/deliver.go:20` (`flushAuto`) | Medium | known issue, by design: inject is off by default, needs `inject_enabled` and a per-dot choice, is clamped for shared and project dots, waits for an idle unfocused pane, never follows a dot-started turn and is capped at 2000 characters (T13, T15) |
| R8 | The Unix socket fallback directory (`$TMPDIR/herdr-dots-<uid>`, used when the state path is too long for a socket) is made with `MkdirAll` and `Chmod` without an owner or symlink check. On Linux, where the temp dir is shared, another user who creates it first makes `Chmod` fail, so the engine never starts. Clients run `Ensure` before they dial, so none connects to a squatted socket | `internal/paths/endpoint_unix.go:44` | Low | follow-up: `Lstat`, refuse a link or a directory another user owns, and say why |
| R9 | `ipc.Listen` makes the socket 0600 after `net.Listen` creates it with the umask's mode | `internal/ipc/endpoint_unix.go:20` | Low | accepted: the parent directory is 0700, so no other user can reach the socket in that window |
| R10 | Project memory and project dot trust are keyed by the git root commit (`g-<root>`), so a fork or hostile clone that shares the root commit reads that project's memory into its dots' prompts and trusts byte-identical project dot files with their granted capabilities | `internal/memory/project.go:98` | Low | accepted, by design (memory follows the project across clones and worktrees, `TestTrustIsSharedByClonesOfTheSameProjectOnly`): memory is only read into prompts and proposals still need approval; trust needs the exact bytes the developer reviewed |
| R11 | `verify-release` takes the cosign identity's repository from `bin/INSTALL_SOURCE` and the checksum from `release/SHA256SUMS` in the same tree: it detects a binary replaced after install, but not a tree rewritten as a whole | `cmd/herdr-dots/verifyrelease.go:141` | Low | accepted: both files are as writable as the binary itself; cosign against the published repository is the real check, and `SECURITY.md` says so |

Totals: Critical 0, High 1 (fixed: R1), Medium 6 (R2 user gate, enforced on a tag; R3 partly
fixed, rest follow-up; R5 follow-up; R4, R6, R7 accepted known issues), Low 4 (R8 follow-up; R9, R10,
R11 accepted).

High and Critical findings are fixed in this task with a test each; the
others are listed in the triage notes' known issues.

## Post-rc status

Status after the post-rc hardening wave and its fix wave (merged at
`e4e38ea`). The table above is the review as written for 1.0.0-rc.1 and is
not changed.

| # | Post-rc status | Tests and what remains |
|---|---|---|
| R3 | FIXED POST-RC | claude-cli runs that read the repository (review, ask, memory, research, council members with repo read, and drafts) deny `Read` of `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.netrc`, `~/.docker/config.json`, `~/.kube`, `~/.claude.json`, `~/.claude` and `~/.codex`, and of the herdr-dots config directory (and the symlink-resolved form when it differs). `TestClaudeRepoReadRunsDenyHomeSecretsAndTheConfigDir` (review, memory pass, ask, research and draft subtests), `TestClaudeHomeDenyNeedsNoConfigDir`. Not live-verified for Grep and Glob (triage X9); reads are still not confined to the working directory |
| R5 | FIXED POST-RC | anthropic-api research declares `web_search` only and never `web_fetch`, with or without the repository tools: `TestWebResearchNeverDeclaresWebFetch`, `TestWebResearchDeclaresServerToolsWithoutStructuredOutput`. Search queries can still carry repository text to the search provider (accepted, P8-R14) |
| R8 | FIXED POST-RC | the fallback directory is `/run/user/<uid>` when that exists and is private (an `XDG_RUNTIME_DIR` counts only at that path, so every client computes the same socket path), else `$TMPDIR/herdr-dots-<uid>`, made with `Mkdir` and checked with `Lstat` (a real directory, this user's, mode 0700); a symlink, a foreign owner or loose permissions is refused with an error naming the path: `TestSockFallbackHappyPathMakesAPrivateDir`, `TestSockFallbackRefusesASymlink`, `TestSockFallbackRefusesADirAnotherUserOwns`, `TestSockFallbackRefusesAnExistingDirOthersCanUse`, `TestSockFallbackPrefersAPrivateXDGRuntimeDir`, `TestSockPathIsTheSameWithOrWithoutAnotherXDGRuntimeDir`. Upgrade note (Linux only, deep state paths only): an old engine and a new client can miss each other until Herdr restarts |
| R2 | FIXED POST-RC | every workflow's `setup-go` installs `go-version: "1.26.9"`; Actions are pinned by commit SHA (checkout v4.4.0, setup-go v5.6.0, upload-artifact v4.6.2, goreleaser-action v6.4.0, cosign-installer v3.10.1) and GoReleaser at v2.18.3. govulncheck v1.1.4 against the Go vulnerability database downloaded 2026-10-10: go1.25.7 (this machine) 24 reachable standard-library vulnerabilities; **go1.26.9 (the workflows' Go): `No vulnerabilities found.`** `go vet` (darwin, windows, soak tag) and `go test -race ./...` pass under go1.26.9. Local builds with an older Go still carry the old standard library: build releases with the workflows. |
| R1 | unchanged | fixed in rc.1 as in the table above |
| R4, R6, R7, R9, R10, R11 | unchanged | accepted or follow-up as in the table above |

## Checked and found sound

No finding in: the jail (`ResolveInRoot`, `openVerified`, `Unavailable`,
exact-name and junction checks), the dot arg denylist and the shared-dot
allowlist, `ClampDot`'s fail-closed rebuild, project dot reading (regular
files, size and count caps, `PreScanTOML` before decode), trust binding to
the shown hash, `memory.Apply`'s op rules and `suppressedBy`'s sources, the
secret scanner's invisible-character pass, `credentials` (descriptor checks,
`O_NOFOLLOW`, no key in TOML errors), `Redact`, pack fetch (https, same
host, no credentials, caps), git pinning (`ExecGit`, `gitFiles`,
`worktree.Git`; commit reviews with `--no-ext-diff --no-textconv`), patch
validation and apply (paths from `git apply --numstat`, no symlinks, private
index, reviewed hash), delivery (sanitized, forged headers neutralised,
idle and focus gates), the MCP server (queries only, proposals only,
redaction, outside-Herdr refusal), codex MCP isolation, `build.sh` and
`build.ps1` (checksum from the cloned tree before extraction, link members
refused, record written last), `verify-release` (last tar member), and the
release workflow's checksum gate before signing. No `InsecureSkipVerify`, no
shell built from model or repository text (the memory editor runs the
developer's own `$EDITOR`), no telemetry.

## Notes for the ready-to-publish checklist

- R1: run one live claude-cli review in a scratch repository whose
  `.claude/settings.json` sets `apiKeyHelper` to a command that writes a
  marker file, and check that no marker appears (with the `--restricted`
  re-check, triage X8). In the same run, check whether the repository's
  `CLAUDE.md` still reaches the dot: the Claude Code bundle suggests it no
  longer does under `--setting-sources user` (inferred, not run).
- R2: set an explicit patched `go-version` in every workflow (`ci.yml`
  and `release.yml`, instead of `go-version-file: go.mod`, which most
  likely installs go1.25.0) and get `No vulnerabilities found.` from
  govulncheck before tagging; the candidate job now runs govulncheck and
  stops on a finding.
- `SECURITY.md` carries `TODO(publish)` placeholders for the reporting
  channel and the repository; replace them when the repository exists.
