# Delivery modes

The delivery mode decides when a dot runs and how its output reaches you.

| Mode | Runs every turn? | Output |
|---|---|---|
| `observe` | no | Answers only when summoned |
| `suggest` | yes | Card you read and copy; toast for warnings |
| `queue` (default) | yes | Card you send with `⏎` |
| `inject` | yes | Sent automatically when the agent is idle and you're not in that pane |

A dot that raises the same finding again (0.7 similarity, the measure memory uses) while its card for it is still waiting on that pane, pending or not yet auto-delivered, adds no second card: the feed shows `suppressed (duplicate of card 12)` and the roster `duplicate`, and summoned answers and council cards are never deduped.
