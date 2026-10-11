# Linux interactive check: popups and keybindings (v1.0.0-rc.1)

**Date:** 2026-10-11.

**Environment:** the real interactive Herdr 0.9.3 client inside tmux 3.5a, in a linux/arm64 container. Keys were pressed with `tmux send-keys`, and the screen was read with `tmux capture-pane`. The prefix is `ctrl+b`, Herdr's default.

**Plugin:** installed with `herdr plugin install GustavoGomez092/herdr-dots --yes`, which installed the rc.1 release binary. The keybindings were added with `setup-keys --yes`. Dot reviews used a stand-in `claude`.

| # | Step | Result |
|---|---|---|
| 1 | `prefix+d` opens manage; the first-run Setup pager offers the MCP server (and the keys when they are missing); `n` snoozes; the library shows | PASS |
| 2 | `t`, Security template, `ctrl+s` saves (the new dot comes back checked), `enter` attaches it to the agent pane | PASS |
| 3 | `prefix+a` toggles the Dots pane; the roster and card show | PASS |
| 4 | A turn produces a second card; `tab` to the roster, then `m` cycles queue → inject → observe → suggest | PASS |
| 5 | `prefix+shift+s` summons Security with a question; the answer arrives as a card | PASS |
| 6 | `prefix+shift+c` convenes a council (one member) with a cost estimate; a council card arrives | PASS |
| 7 | `M` memory screen shows the sent finding and the dismissed one; `E` export defaults to the home directory; the export writes a 0600 file | PASS |
| 8 | At 60×15, the export screen says "Too small" and refuses `enter`; growing back restores it; summon and council stay usable | PASS |

**Found:**
- **B1. Fast keys dropped.** Several keys arriving in one read become one multi-rune key message, which matches no binding.
- **B2. Wrong pane width on open.** A newly toggled Dots pane draws at its pre-split width until focused. This is Herdr 0.9.3's post-split resize applied late.
- **B3. Wrapped card lines lose their indent.**
- **B4. Partial row highlight.** Only the glyph of the selected roster row is highlighted.
- **B5. Footers wrap mid-word.**
- **B6. The narrow Setup "Too small" screen loses its top lines.**
- **Small wording issues:**
  - "1 dots";
  - raw `##` markdown in a council card;
  - the memory key line stays visible under the export prompt;
  - the Setup "keys are at the end" hint shows when everything fits.

Fixes follow on main.
