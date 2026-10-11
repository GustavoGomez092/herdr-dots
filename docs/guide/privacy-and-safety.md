# Privacy and safety

Every rule about what leaves your machine, what dots may touch and what needs your approval.

- Dots send recent conversation turns, their memory block and (with repo access) the files they read to
  **their own** provider: Anthropic for `claude-cli` and `anthropic-api`, OpenAI for `codex-cli` and
  `openai-api`, OpenRouter and the model vendor it routes to for `openrouter`. `ollama` stays on your
  machine unless its `base_url` points elsewhere.
- OpenAI requests use `store: false`; nothing is kept on OpenAI's side between tool turns.
- Dots are read-only unless you turn on writing twice (`write_enabled` and a dot's
  `[capabilities] write`). Reviews, councils, MCP asks, summons and memory passes always stay
  read-only: claude dots get only `Read,Grep,Glob`, codex dots run with `--sandbox read-only`, API
  dots get read-only tools.
- A draft runs in a git worktree outside your repository, made from a snapshot of your working
  tree (uncommitted and untracked files included) through a private index. Your index, stash,
  branches, `HEAD` and commits never change. What is added to the repository: objects, a private
  ref (`refs/herdr-dots/snapshots/<id>`), and, while the draft runs, git's own registration files
  for the worktree under `.git/worktrees/`. The ref goes when the patch is applied or discarded,
  stays 7 days after the patch is sent to the agent, and stays for a pending card, or one whose
  pane closed, until you apply or discard it. A daily sweep removes refs no patch owns.
  Git hooks and fsmonitor programs never run for it.
- No dot ever gets a shell. A claude draft gets `Read, Grep, Glob, Edit, Write` and runs with
  `--restricted` (your and the repository's Claude settings files are ignored), and only edits
  inside its worktree are accepted; API drafts get `write_file`/`edit_file` jailed to the worktree.
  Symlinks that point outside the repository are replaced while the dot works.
- Patches are validated before you see them. They are rejected when they touch `.git` or agent
  configuration (`.claude/`, `.mcp.json`, `.codex/`, for every backend), reach outside the
  repository, hit the deny list (secret-looking files such as `.env`), create or change
  any symlink, or go over the size or file caps. (Deleting a symlink, or replacing it with a regular
  file, is allowed.) Every draft needs two approvals: one to start, one (with the diff on screen)
  to apply. Applying touches the working tree only; it never stages or commits.
- Hooks are disabled for dot runs, and Herdr variables are stripped, so dots never affect your panes.
- `claude-cli` review, ask, memory, council and chair runs ignore the repository's
  `.claude/settings.json` and `.claude/settings.local.json` (`--setting-sources user`), so a
  repository can't run a credential helper or redirect a dot (live-verified 2026-10-11 with Claude Code 2.1.296: in a repository whose `.claude/settings.json` names an `apiKeyHelper`, plain `claude -p` ran it 11 times and a dot review ran it 0 times); the repository's `CLAUDE.md` doesn't reach them as instructions either (verified the same day: the dot read it only as a repository file, flagged it and didn't follow it) (see
  [Backends](backends.md#claude-cli-dots-ignore-the-repositorys-claude-settings)).
- `claude-cli` runs that read files are denied secret files in the repository and your home
  secrets outside it (`~/.ssh`, `~/.aws`, `~/.claude`, the herdr-dots config directory with
  `credentials.toml`, and more; see [Backends](backends.md#claude-cli-dots-are-denied-your-home-secrets)).
- Queue mode (you approve) is the default. Inject mode is opt-in per dot and can be turned off for
  all dots with `inject_enabled = false` in `config.toml`.
- Changes to `config.toml` apply within a few seconds, without a restart (except `max_parallel_runs`, the `memory_*` limits and turning `memory_enabled` back on);
  if the file can't be parsed, the last good settings stay and inject is turned off until it is fixed.
- Per-dot and per-session call budgets and dollar budgets (`max_usd_per_session`, `session_budget_usd`)
  cap cost. Runs already in flight can overshoot a dollar budget slightly.
- Council members in round 2 see the other dots' answers, framed as opinions, never as instructions.
  The council chair sees the members' answers, the recent turns and the project's approved
  conventions and decisions; it has no repo access and writes no memory.
- Council cards are never sent without your approval, whatever the members' delivery modes are.
- With the MCP server installed, the main agent can read dot answers, the queue and approved memory
  **without your approval** (see "Dots MCP server"); dots still never change files, and agent-written
  memory is always a proposal.
- Memory stays on this machine. It leaves it only inside the prompts a dot sends to **its own**
  provider, and a dot on a remote provider receives project memory written by other dots and by you.
- Dots can't plant shared conventions: model-written project memory waits for your approval, and
  dots can't write "don't raise this" rules or global memory.
- Text that looks like a credential is refused when a dot proposes it, and needs `--force` (CLI) or a
  second save (popup) when you add it. A card that quotes a secret is never remembered.
- Forgetting is real: `forget` removes the entry from disk, `--scrub` also removes its ledger lines,
  and `purge` deletes a project's whole memory.
  Deleting a dot's memory in all projects (the manage popup) also removes the ledger lines its
  cards left and the project conventions it proposed that you never approved. It also deletes the
  copies of that dot's memory file that were set aside as damaged (`<dot>.json.corrupt-*`).
  A damaged project file set aside as `project.json.corrupt-*` mixes every dot's entries and is
  never edited, so it may still hold a deleted dot's text: `herdr-dots memory purge` is what
  clears those.
- Memory text is stripped of control characters (terminal escape sequences) when it is written and
  when it is read, like the feed.
- Proactive dots are opt-in per dot and read-only, even when the dot is write-capable: research,
  scheduled runs and commit reviews never draft. With `[research] web = true`, research sends search
  queries derived from your code to the backend's web search provider. `claude-cli` and `anthropic-api` research use web
  search only, with no page fetching, and secret files are denied to their reads. Background spend is capped by
  `daily_budget_usd` (default $1.00) across every Herdr session.
- Commit reviews never install git hooks; they only read `HEAD` and `git log`/`git show` output, with
  the repository's hooks, fsmonitor, diff drivers and transports disabled.
- Shared dots are someone else's prompts: imported and project dots start with every risky
  capability off, backend args other than `--model` and `--effort` are dropped, keys this version
  doesn't understand are refused, and project dot trust is tied to the file's exact content.
- Dotpacks never carry credentials or history, exported memory is secret-scanned, and history
  stays on this machine with keys redacted.
- Commit reviews leave out the content of files the deny set covers (see [Proactive dots](proactive.md#commit-reviews-leave-denied-files-out)).
- An invalid `tool_deny_globs` entry turns repository tools, drafts, research and commit patches off until it is fixed (see [Backends](backends.md#an-invalid-tool_deny_globs-entry-turns-repository-tools-off)). Codex dots read the repository through Codex's own sandbox, which `tool_deny_globs` does not limit.
- A file written by a newer herdr-dots is never overwritten by an older one: it stays read-only and you get a notice (see [Install and upgrade](install-and-upgrade.md#upgrading)).
