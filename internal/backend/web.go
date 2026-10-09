package backend

import "errors"

// WebSupported reports whether a backend kind can give a research pass web
// tools: the claude CLI (WebSearch only, no page fetching, ruling P8-R14;
// its research runs --restricted with Read denied for secret files), the
// Anthropic API (server web search and fetch) and the OpenAI API (hosted web
// search). Search queries go to the provider. Codex (its
// web flag is not verified, Phase 8 V3), OpenRouter and Ollama research
// without the web; the engine logs that and the editor warns.
func WebSupported(kind string) bool {
	switch kind {
	case KindClaudeCLI, KindAnthropicAPI, KindOpenAIAPI:
		return true
	}
	return false
}

// checkResearchNotWrite refuses a research pass that also asks for write
// tools: research is read-only and never mixes with a draft.
func checkResearchNotWrite(r Request) error {
	if r.Pass == PassResearch && r.Write {
		return errors.New("backend: a research pass cannot be combined with write tools")
	}
	return nil
}
