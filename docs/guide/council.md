# Council mode

Council mode asks every attached dot at once and returns one merged digest card.

Ask every attached dot the same question at once and get **one** card back. Press `C` in the Dots
pane, run the `herdr-dots.council` action (`prefix+shift+c` after `setup-keys`), or pick
"⊕ Council (all attached)" at the top of the summon popup.

The popup has an optional question (empty means "review the recent turns") and a checklist of
members: every attached dot that isn't paused or out of budget, observe-mode dots included. It shows
the cost before anything runs (members × rounds, plus one call for the chair). If the pane's session
budget can't cover it, Enter stays disabled and the popup says why.

Members answer in parallel (within `max_parallel_runs`) and may pass. A **chair** run merges the
answers into a digest with `## Must fix`, `## Should fix`, `## Consider`, `## Disagreements` and
`## Not heard from`, each point attributed to the dots that raised it. If the chair fails or returns
something malformed, a deterministic plain merge is used and the card says so. Press `space` on the
council card to see each member's own answer; a member that failed shows `✗ timed out`.

The council card is always a queue card: nothing is sent until you press `⏎`. It arrives in the
agent as `[⊕ Council dot] …`, and no dot reviews the turn it starts. Sending it records an accepted
finding for each dot that contributed; dismissing it records a dismissal for each (see Dot memory).

Settings in `config.toml`:

```toml
[council]
chair_backend = ""      # empty = default_backend
chair_model   = ""      # empty = default_model
rounds        = 1       # 2 = dot-to-dot cross-review: each dot sees the others' answers and replies AGREE/OBJECT/ADD
max_members   = 8       # 2-16
merge         = "chair" # chair | plain
```

`rounds = 2` roughly doubles the cost. One council runs per pane at a time. A council in progress is
lost if the engine restarts (calls already made still count against the budgets).
