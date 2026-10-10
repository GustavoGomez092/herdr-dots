package anthropicapi

import "github.com/anthropics/anthropic-sdk-go"

// Web research (Phase 8).
const (
	// WebSearchUSD is one server web search: $10 per 1,000 searches (as of
	// 2026-10-08). Research declares no web_fetch (security review R5).
	WebSearchUSD = 0.01
	// webMaxUses is the cap on paid web uses per research Run in total,
	// across tool-loop turns and pause_turn resumes (P8-R6). A fetch the
	// server still reports is counted too.
	webMaxUses = 5
	maxPauses  = 5 // pause_turn resumes per call
)

// webUses counts the web searches and fetches a response paid for: the larger
// of the usage counters and the server_tool_use blocks in the content, so a
// gateway that drops usage.server_tool_use cannot zero the cost.
func webUses(resp *anthropic.BetaMessage) (searches, fetches int) {
	var bs, bf int
	for _, block := range resp.Content {
		if v, ok := block.AsAny().(anthropic.BetaServerToolUseBlock); ok {
			switch string(v.Name) {
			case "web_search":
				bs++
			case "web_fetch":
				bf++
			}
		}
	}
	return max(int(resp.Usage.ServerToolUse.WebSearchRequests), bs), max(int(resp.Usage.ServerToolUse.WebFetchRequests), bf)
}
