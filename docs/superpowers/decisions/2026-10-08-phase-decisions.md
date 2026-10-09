# Phase decisions (made autonomously on the user's behalf)

- **Date:** 2026-10-08
- **Context:** The user asked for every remaining phase to be built autonomously, with best judgment in place of review questions. These rulings resolve the "Open decisions for the user" in the Phase 2–10 draft specs (`docs/superpowers/specs/2026-10-08-herdr-dots-phase-*-design.md`). For each item I record what I decided, why, and the cost if it's wrong. Every ruling can be reversed. The draft specs are now treated as **approved, with the amendments below**.

| # | Decision | Ruling | Why | Cost if wrong |
|---|---|---|---|---|
| 1 | Product name / rename before publishing (P10 O1, O2) | **Keep "Herdr Dots"** (`herdr-dots`). No rename and no noun change in Phase 10. Phase 10 still adds a data-migration framework, so a later rename is cheap. | The user named the product. A rename is an identity and branding choice that only matters when publishing, and publishing stays the user's decision. | A rename later touches the plugin id, the binary name and the docs. The migration framework from P10 keeps it lossless. |
| 2 | Model-written project memory (P2 O2) | **Proposed until approved** | Prompt-injected or wrong "conventions" must never become shared truth silently. | Users approve proposals by hand, which is a small amount of friction. |
| 3 | Include drafter-proposed Phases 8 and 9 in "feature complete" (P8 O5, P9 O1) | **Yes, both** | The user asked for the full product. Proactive dots and profiles/sharing close the gap with OpenAI Dots (proactive research, history). | More build time. |
| 4 | Windows position (P6 O1) | **Phase 6, before Phases 7–9** | Later phases are then written cross-platform once. | Windows can only be cross-compiled and unit-tested here, not run. That gap is recorded in P6 and P10. |
| 5 | Shell access for write-capable dots (P7 O1) | **None** | Smallest blast radius. Edits are reviewed as patches. | Some refactors need a shell; they can be added later behind an explicit flag. |
| 6 | API key storage (P4 O1) | **`credentials.toml` (0600) plus env indirection**, keychain later | Portable, testable, no OS-specific dependencies. | Keys are at rest in a file, mitigated by the 0600 mode and a warning in the docs. |
| 7 | Default Anthropic model for API dots (P4 O2) | **`claude-opus-5-5`** with per-dot `effort` | It is the most capable current model; cost is controlled per dot. | Higher spend for users who don't lower `effort`. Budgets cap it. |
| 8 | Anthropic refusal fallbacks on by default (P4 O3) | **On** | A dot quietly returning nothing is worse than a fallback answer. | A fallback model answers occasionally. This is logged in the feed. |
| 9 | MCP reach and `list_queue` (P5 O1, O2) | **Attached dots only. `list_queue` on**, with a README warning that MCP answers bypass the approval queue | Least privilege, while keeping the main agent useful. | — |
| 10 | Schedules and daily budget (P8 O1, O2) | **Only while Herdr runs, with catch-up. $1.00 per day by default** | No background daemon outside Herdr. Bounded spend. | Missed runs while Herdr is closed are caught up at the next start. |
| 11 | Project identity (P2 O1) | **Git root commit, path-hash fallback** | Clones and worktrees of one repo share memory. | Unrelated repos with the same root commit (forks) share memory. This is documented. |
| 12 | Observe-mode learning cost (P2 O3) | **One memory pass every 5 turns, at most 10 per session** | Learning stays cheap and bounded. | Learning is slower. |
| 13 | Anti-repeat suppression (P2 O5) | **On at 0.7 similarity, logged in the feed** | The core promise is that dots don't repeat dismissed advice. | Occasional over-suppression, which is visible in the feed. |
| 14 | Council defaults (P3 O1–O5) | **Observe dots included, 1 round, chair = default backend/model, members not forced** | Drafter defaults; cheap and predictable. | — |
| 15 | Write-apply semantics (P7 O3) | **Working tree only. Never stage or commit** | The user stays in control of git history. | The user stages changes themselves. |
| 16 | License (P10 O3) | **Not chosen. No LICENSE file is added** | A license is the owner's legal decision. Phase 10 lists it as a release blocker for the user. | Publishing waits on the user. |
| 17 | Project dots (P9 O2, O3) | **Supported, with content-hash trust. Risky capabilities clamped on import** | Shareable team dots without silent code-execution risk. | — |

## Release-gate amendments (Phase 10)

- **No outward-facing action is taken autonomously:** no `git push`, no remote tag, no GitHub topic, no marketplace listing, no GoReleaser publish. Phase 10 builds everything up to the point of release: GoReleaser config, checksums, the migration framework, fuzzing, the threat model, the soak test and the acceptance checklist. It stops at a written "ready to publish" checklist for the user.
- **The three-OS acceptance run cannot run on this macOS machine.** Linux and Windows are covered by cross-compilation and unit tests. A real run on those systems is listed as a remaining gate for the user, and "feature complete" is stated with that caveat.
- **Codex CLI is not installed here.** Codex paths stay "documented-format, not live-verified" until the user runs the live tests.
