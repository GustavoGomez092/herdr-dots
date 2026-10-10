# Profiles, history and sharing

Per-dot run history and profiles, dotpacks to export and import dots, and project dots shipped in a repository.

Every dot run (reviews, summons, council members, MCP asks, research, scheduled and commit runs,
drafts and memory passes) and every card outcome (sent, edited, dismissed, applied, discarded,
injected, orphaned) is kept per dot in the plugin state dir under `history/<slug>.jsonl`, across
sessions and projects, capped at 20,000 lines. A run dropped before it ended (its pane closed,
the dot detached) is recorded as "dropped" together with any cost it incurred. History holds only
300-character excerpts, with anything that looks like a key always redacted, and never leaves this
machine.

Press `i` on a dot in the library for its **profile**: stats over rolling 7- and 30-day windows
(`tab` switches; the last 7 or 30 days from now, not calendar days) with runs by kind, how often it
flags, how often you accept its advice, suppressed repeats, cost and median latency; a memory
summary; plain tuning hints ("Mostly dismissed", "Rarely speaks", "Your most expensive dot", "Keeps
repeating dismissed advice", "Fails often"); and its recent history (`enter` shows the full
message). The same data is available from the shell: `herdr-dots dots profile <slug>`.

There are 14 built-in templates (`t` in the library). In every template, write, research, web,
schedules and commit reviews are off; MCP asks from your agent are allowed (your own dots).

**Share a dot** as one `.dotpack.toml` file (a dotpack): `x` in the profile, `I` in the library to
import, or

```sh
herdr-dots dots export security --with-memory dot -o security.dotpack.toml
herdr-dots dots import security.dotpack.toml            # or an https:// URL
```

Export refuses a dot or memory entry that looks like it holds a secret and names the fields only,
never the text; `--force` drops the flagged memory entries and exports the rest. Absolute paths
(`schedule.projects`) are dropped. Packs never contain credentials, history, global memory or
preferences. Fetching a pack by URL is https only (a redirect that leaves https is refused), and a
redirect to another host is refused too: every redirect must stay on the host you typed. Some
raw-file hosts redirect downloads to a CDN on another host, so their URLs are refused; download
the file and import it from disk instead (`herdr-dots dots import ./file.dotpack.toml`).

Import clamps every risky capability by default: a shared dot keeps only `--model` and `--effort`
backend args, its budgets are clamped, and **inject delivery, write, MCP, research, web, schedules,
commit triggers and custom `base_url`s are turned off**. Keep one with `--keep inject` (CLI) or its
number in the preview. The preview shows everything (purpose, research topics, schedule task,
backend, every memory entry) and must be read to the end before you can confirm. An import never
overwrites a dot: a slug collision is refused (`--as <name>` imports it under another name). Memory
in a pack arrives only as proposals labelled "imported"; they stay out of prompts until you approve
them (`herdr-dots memory approve`, or `a` in the memory browser) and never stop a dot from repeating
advice. Control, bidi and other invisible characters are refused in shared dots and in imported
memory.

**Project dots in a repository:** put dot files in `.herdr-dots/dots/*.toml` at the repository root
(at most 64 files are read per repository). They show in the library as `project` dots, but a file
runs only after you **trust** the exact bytes you saw: `T` in the library (or `herdr-dots dots trust
<file>`) shows the whole file and the capabilities it asks for, each off until you grant it. Any
edit makes the file untrusted again and pauses the dot until you trust it again; an untrusted or
changed project dot never runs, in the background either. Trust is per project (the repository's
git root commit): clones share it, but only for byte-identical files, and when trust came from
another checkout the popup shows "trusted at <path>". `herdr-dots dots untrust <file>` revokes
the trust of that checkout's file: its current bytes (so a clone holding the same bytes loses it
too), or, when the file changed, is gone or no longer parses, the trust given at that path. Another
checkout's trust of different bytes stays. Trusted dots still run clamped, with only the
capabilities you granted. `project_dots_enabled = false` in `config.toml` turns project dots off
(a malformed `config.toml` does too).

**Upgrading from 0.8:** 0.9.0 refuses some backend args in every dot, your own local dots
included, because they could redirect a dot's prompt or widen what it may do. A local dot that uses
one now stops loading: `herdr-dots dots list` shows it with "backend args … are not allowed".
Remove the arg from `backend.args` in the dot's file to load it again. Newly refused:

- claude: `--system-prompt-file` (put the text in `purpose` instead).
- codex: `-c`/`--config` keys other than `model`, `model_reasoning_effort`,
  `model_reasoning_summary` and `hide_agent_reasoning`; `--search`; `-p`/`--profile`;
  `-o`/`--output-last-message`; `--oss`; `--local-provider`; `--enable`/`--disable`.

The live check of the manage popup (profile, import, trust) was **not run on this machine**; it is
a user gate (see `docs/superpowers/notes/2026-10-08-phase-9-verification.md`).
