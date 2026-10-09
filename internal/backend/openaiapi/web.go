package openaiapi

// WebSearchCallUSD prices one hosted web search call for the daily dollar
// budget. It is the highest per-call rate OpenAI published as of 2026-10-08
// ($25 per 1,000 calls), so the budget never under-counts (Phase 8 V6).
const WebSearchCallUSD = 0.025

// webMaxCalls caps hosted web search calls per research Run in total, across
// tool-loop turns (ruling P8-R6).
const webMaxCalls = 5
