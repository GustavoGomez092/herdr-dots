// Package all registers every API backend. Import it for side effects:
//
//	import _ "herdr-dots/internal/backend/all"
package all

import (
	_ "herdr-dots/internal/backend/anthropicapi"
	_ "herdr-dots/internal/backend/ollama"
	_ "herdr-dots/internal/backend/openaiapi"
	_ "herdr-dots/internal/backend/openrouter"
)
