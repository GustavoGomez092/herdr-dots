package anthropicapi

// Web research (Phase 8).
const (
	// WebSearchUSD is one server web search: $10 per 1,000 searches (as of
	// 2026-10-08). Web fetch adds only the tokens it reads.
	WebSearchUSD = 0.01
	webMaxUses   = 5 // searches, and fetches, per research request
	maxPauses    = 5 // pause_turn resumes per call
)
