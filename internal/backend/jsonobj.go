package backend

import (
	"encoding/json"
	"strings"
)

// maxCandidates bounds the fallback scan: the '{' positions it tries.
const maxCandidates = 64

// jsonSpan returns the JSON object in model text. The v1 rule comes first and
// is unchanged: the span from the first '{' to the last '}', when it parses.
// When it doesn't (prose braces around the JSON), a string-aware scan takes
// the one balanced {...} that parses as an object with key at its top level
// (B1, ruling P8-R7). ok is false with no span at all; with a span that
// doesn't parse and no single candidate, span is the v1 span (its parse
// error is the reported one), so two objects never parse as one.
func jsonSpan(text, key string) (span string, ok bool) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", false
	}
	span = text[start : end+1]
	if json.Valid([]byte(span)) {
		return span, true
	}
	var found []string
	tried := 0
	for i := start; i < len(text) && tried < maxCandidates && len(found) < 2; i++ {
		if text[i] != '{' {
			continue
		}
		tried++
		j := closingBrace(text, i)
		if j < 0 {
			continue
		}
		if c := text[i : j+1]; hasTopKey(c, key) {
			found = append(found, c)
			i = j // an object nested inside a candidate is not another one
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return span, true
}

// closingBrace is the index of the '}' that closes the '{' at text[i],
// skipping braces inside JSON strings; -1 when it never closes.
func closingBrace(text string, i int) int {
	depth, inStr, esc := 0, false, false
	for j := i; j < len(text); j++ {
		c := text[j]
		switch {
		case inStr && esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case inStr && c == '"':
			inStr = false
		case inStr:
		case c == '"':
			inStr = true
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// hasTopKey reports whether s is a JSON object with key at its top level.
func hasTopKey(s, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
