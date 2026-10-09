package backend

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseReply parses a review reply: the verdict exactly as ParseVerdict does,
// plus up to MaxReviewOps valid memory ops from an optional "remember" array.
// Invalid ops are dropped and a "remember" that isn't an array is ignored:
// memory problems never fail the verdict.
func ParseReply(text string) (Verdict, []MemoryOp, error) {
	v, err := ParseVerdict(text)
	if err != nil {
		return Verdict{}, nil, err
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	var raw struct {
		Remember json.RawMessage `json:"remember"`
	}
	if json.Unmarshal([]byte(text[start:end+1]), &raw) != nil || raw.Remember == nil {
		return v, nil, nil
	}
	ops, _ := decodeOps(raw.Remember, MaxReviewOps)
	return v, ops, nil
}

// ParseMemoryOps parses a memory-pass reply {"remember":[...]} into up to
// MaxPassOps valid ops. No JSON object, invalid JSON, a missing "remember" or
// a non-array "remember" is ErrMalformed (a failed run, like a bad verdict).
func ParseMemoryOps(text string) ([]MemoryOp, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("%w: no JSON object in %q", ErrMalformed, clipErr(text))
	}
	var raw struct {
		Remember json.RawMessage `json:"remember"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if raw.Remember == nil {
		return nil, fmt.Errorf("%w: missing \"remember\"", ErrMalformed)
	}
	ops, ok := decodeOps(raw.Remember, MaxPassOps)
	if !ok {
		return nil, fmt.Errorf("%w: \"remember\" is not an array", ErrMalformed)
	}
	return ops, nil
}

// decodeOps keeps the first limit well-formed ops; ok is false when raw is
// not a JSON array (null counts as an empty array).
func decodeOps(raw json.RawMessage, limit int) ([]MemoryOp, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	var out []MemoryOp
	for _, it := range items {
		if len(out) == limit {
			break
		}
		var op MemoryOp
		if json.Unmarshal(it, &op) != nil {
			continue
		}
		if op, ok := cleanOp(op); ok {
			out = append(out, op)
		}
	}
	return out, true
}

func cleanOp(op MemoryOp) (MemoryOp, bool) {
	op.Op = strings.ToLower(strings.TrimSpace(op.Op))
	op.ID = strings.TrimSpace(op.ID)
	op.Scope = strings.ToLower(strings.TrimSpace(op.Scope))
	op.Kind = strings.ToLower(strings.TrimSpace(op.Kind))
	op.Text = strings.TrimSpace(op.Text)
	var paths []string
	for _, p := range op.Paths {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	op.Paths = paths
	switch op.Op {
	case "add":
		return op, op.Text != ""
	case "update":
		return op, op.ID != "" && op.Text != ""
	case "forget":
		return op, op.ID != ""
	}
	return op, false
}

// finishResult fills the verdict and memory ops from a model's reply text.
func finishResult(res Result, text, pass string) (Result, error) {
	if pass == PassMemory {
		ops, err := ParseMemoryOps(text)
		res.Remember = ops
		return res, err
	}
	v, ops, err := ParseReply(text)
	res.Verdict, res.Remember = v, ops
	return res, err
}
