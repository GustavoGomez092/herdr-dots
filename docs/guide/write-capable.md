# Write-capable dots (drafts)

A write-capable dot can draft a fix in a sandboxed git worktree, which you review and apply yourself.

A dot you trust can draft a fix instead of only describing it. It edits an isolated copy of your
repository; nothing changes in your files until you read the diff and apply it. Drafts are built and
unit-tested; a live Claude draft has **not been run** on the developer's machine yet, and on Windows
drafts are compile-checked and unit-tested only.

**Turn it on (both switches are off by default):**

1. In the plugin's `config.toml`: `write_enabled = true`. A malformed `config.toml` turns it off
   again until you fix the file. `max_concurrent_drafts` (default 2) limits drafts at once.
2. In the dot's file, `dots/<slug>.toml` (there is no switch for this in the manage popup, on
   purpose):

   ```toml
   [capabilities]
   write                 = true
   write_timeout_seconds = 600     # 60-3600
   max_patch_bytes       = 200000  # larger patches are rejected
   max_files             = 50      # patches touching more files are rejected
   ```

   `claude-cli` and the API backends can draft. `codex-cli` dots can't: `codex exec` always has a
   shell, and write-capable dots never get one. A `claude-cli` draft needs a Claude Code version
   that supports `--restricted` (2.1.295 is verified); an older one refuses the flag and the draft
   fails without running.

**Draft.** In the Dots pane, select a card and press `w`, or select a write-capable dot in the
roster and press `w` and type what to fix. The confirm shows the dot, backend, model, time limit,
the tools it gets (no shell) and the full instruction; `y` starts it, `e` edits the instruction,
`tab` picks another write-capable dot, `esc` cancels. An instruction over 4,000 characters is
refused, not shortened, so what you confirmed is exactly what runs. While it runs the roster shows
`✎ Security drafting… 1:12`. A draft counts against the dot's and the pane's budget like any call,
and closing the agent's pane cancels its draft.

**Review.** The result is a `PATCH` card with a diffstat and flags such as `[binary]`, `[mode]`
or `[symlink]`. A patch that changes agent or CI configuration (CI workflows, `.vscode` tasks and
settings, `.envrc`, `.gitattributes`, `.gitmodules`, pre-commit and husky hooks, `Makefile`,
`package.json`, `.npmrc`) is flagged `[agent/CI config]` first: those files run code the next time
CI, your editor, git or a package script starts.

- `v` opens the patch viewer: the changed files on the left, the diff on the right, `n`/`p` for
  the next and previous hunk. Long lines wrap onto rows marked `↪`, never cut, and a file counts
  as viewed only once every row of it has been on screen.
- `A` opens the viewer with *Apply N files to your working tree? Nothing is staged or committed.
  (y/N)*; `y` applies. The confirm lists the files you haven't viewed yet, so you can see what you
  are skipping. The apply request carries the hash of the patch you reviewed, and the engine
  refuses it if the patch changed since. Apply runs only while the agent pane is idle and only into
  the repository the patch was drafted for. If the files moved since the draft, it merges three
  ways; files it can't merge keep conflict markers, and the card, the feed and a toast list them.
  Your index, branches and commits never change: review and commit as usual.
- `⏎` sends the agent a short note instead: *Patch ready: 3 files +42 −7. Review with
  `git apply --stat "<path>"`, apply with `git apply "<path>"`.* The diff itself never goes into
  the agent's context.
- `x` discards the patch.

Memory: an applied patch becomes a finding ("Applied patch: …") that keeps the dot from repeating
the advice; a discarded one leaves only a ledger line.
