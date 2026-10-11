# Linux acceptance run (v1.0.0-rc.1)

**Date:** 2026-10-11.

**Environment:** linux/arm64 (Debian 13.7, `golang:1.26.9` image, no Go toolchain used by the install), with Herdr 0.9.3 headless (`herdr --session acc server`). The plugin was installed the real way:

```
herdr plugin install GustavoGomez092/herdr-dots --yes
```

That installed the verified release binary `herdr-dots 1.0.0-rc.1 (release, commit 6cb7e36118ce)`.

**How the run was driven:**
- **A stand-in `claude`:** a script on PATH that returns a fixed verdict in the claude-cli JSON format. Its format was checked with the product's own `backend.ParseClaudeOutput`.
- **Agent state:** Herdr's claude screen detection, plus `pane report-agent-session` for the session id.
- **Engine actions:** a small client of the engine socket, doing the same as the manage popup's attach, send and dismiss.

| # | Step | Result |
|---|---|---|
| 1 | Install from GitHub, `--version` says `release`, `INSTALL_SOURCE` checksum matches `release/SHA256SUMS` | PASS |
| 2 | The engine starts with the session (plugin startup `engine --ensure`, exit 0; `engine.log`) | PASS |
| 3 | Attach the Security dot to a Claude agent pane; the catch-up review runs | PASS |
| 4 | `plugin action invoke herdr-dots.toggle-pane` opens the Dots pane; the roster shows Security and its card | PASS |
| 5 | A turn ends, the dot runs (argv includes `--setting-sources user`, `--strict-mcp-config`, the hookless settings with the home-secret Read denies, `--tools Read,Grep,Glob`), and the card appears in the pane and `queue.json` | PASS |
| 6 | Sending a card types it into the agent pane, both by the Dots pane's `enter` and through the engine; the outcome is remembered (`mem 1`) | PASS |
| 7 | Dismissing a card remembers it; the same finding on a later turn is `suppressed` (feed: `similar to d:m2`); a sent finding is suppressed the same way | PASS |
| 8 | The manage popup opens, but Herdr 0.9.3 can't read popups through `pane read`. Run in a normal pane with the popup's environment, it shows the first-run Setup offer, the library and the memory view (`d:m1 finding`, `d:m2 dismissed`) | PARTIAL (Herdr limit) |

**Observations:**
- **Duplicate pending cards.** A finding raised again while its identical card is still pending adds a second card. Memory suppression starts only after a card is sent or dismissed.
- **Roster wording.** The roster shows `passed` after a turn whose finding was suppressed; the feed says `suppressed`.

Both are fixed after rc.1 (post-rc): a repeat of a still-waiting card adds no second card, and the roster reads `suppressed` or `duplicate`. Tests: `TestRepeatWhileCardPendingAddsNoSecondCard`, `TestDismissedCardStillSuppressedByMemoryNotDeduped`, `TestDifferentFindingWhileCardPendingAddsCard`, `TestOtherDotSameTextAddsCard` and `TestSummonIsNeverDeduped` in `internal/engine/pending_dedupe_test.go`.

**Herdr 0.9.3 behaviour seen (not Dots issues):**
- Popups have no pane id for `pane read`.
- A report source that once sent `--seq` has later reports without a seq dropped silently.
- `pane release-agent --source` fails from the CLI but works through the socket API.
