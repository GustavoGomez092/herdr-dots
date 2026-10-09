package backend

import "sort"

// VerdictSchema is the JSON schema API backends request as structured output
// for r: the verdict (plus "remember" when r.Learn), or {"remember":[...]}
// for a memory pass. It is strict-mode safe (every object closed, every
// property required) so OpenAI, OpenRouter, Anthropic and Ollama all accept
// it. ParseReply still validates every reply.
func VerdictSchema(r Request) map[string]any {
	remember := map[string]any{"type": "array", "items": memoryOpSchema()}
	if r.Pass == PassMemory {
		return closed(map[string]any{"remember": remember})
	}
	props := map[string]any{
		"relevant": map[string]any{"type": "boolean"},
		"severity": map[string]any{"type": "string", "enum": []any{"info", "warn", "critical"}},
		"message":  map[string]any{"type": "string"},
	}
	if r.Learn {
		props["remember"] = remember
	}
	return closed(props)
}

// memoryOpSchema mirrors MemoryOp. Fields an op doesn't use are sent as ""
// or []; ParseReply's cleanOp drops invalid ops.
func memoryOpSchema() map[string]any {
	str := map[string]any{"type": "string"}
	return closed(map[string]any{
		"op":    map[string]any{"type": "string", "enum": []any{"add", "update", "forget"}},
		"id":    str,
		"scope": str,
		"kind":  str,
		"text":  str,
		"paths": map[string]any{"type": "array", "items": str},
	})
}

func closed(props map[string]any) map[string]any {
	req := make([]any, 0, len(props))
	for _, k := range sortedKeys(props) {
		req = append(req, k)
	}
	return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
