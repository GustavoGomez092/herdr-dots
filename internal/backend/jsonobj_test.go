package backend

import (
	"errors"
	"testing"
)

// B1 (post-rc hardening, ruling P8-R7): a reply with prose braces around its
// JSON used to be malformed (the first '{' to the last '}' is not JSON). The
// parsers now fall back to a string-aware scan and take the one balanced
// object that parses and has the required key; none, or more than one, is
// still malformed (fail closed: two objects never parse as one).
func TestProseBracesAroundTheJSONStillParse(t *testing.T) {
	text := "Checked the {auth} module.\n```json\n" +
		`{"relevant":true,"severity":"warn","message":"Use {id} not \"}\" in paths","remember":[{"op":"add","scope":"dot","kind":"finding","text":"Paths use {id}."}]}` +
		"\n```\nSee {docs}."
	v, err := ParseVerdict(text)
	if err != nil || !v.Relevant || v.Severity != "warn" || v.Message != `Use {id} not "}" in paths` {
		t.Fatalf("ParseVerdict = %+v, %v", v, err)
	}
	v2, ops, err := ParseReply(text)
	if err != nil || v2 != v || len(ops) != 1 || ops[0].Text != "Paths use {id}." {
		t.Fatalf("ParseReply = %+v %+v, %v", v2, ops, err)
	}
	ops, err = ParseMemoryOps(`Plan {x}: {"remember":[{"op":"add","scope":"dot","kind":"finding","text":"Keep {it}."}]} end }`)
	if err != nil || len(ops) != 1 {
		t.Fatalf("ParseMemoryOps = %+v, %v", ops, err)
	}
}

func TestTwoCandidateObjectsStayMalformed(t *testing.T) {
	for _, text := range []string{
		`Example {"relevant":false} then {"relevant":true,"severity":"critical","message":"real"} {x}`,
		`{"relevant":true,"message":"a"} {oops} {"relevant":true,"message":"b"}`,
		`no object {here} at all {`,
		`Format: {"relevant": true, "message": "..."}. My answer: {"relevant": false, "message": "none",}`,
	} {
		if _, err := ParseVerdict(text); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseVerdict(%q) err = %v, want ErrMalformed", text, err)
		}
	}
	if _, err := ParseMemoryOps(`{"remember":[]} {x} {"remember":[]}`); !errors.Is(err, ErrMalformed) {
		t.Errorf("two memory objects: err = %v", err)
	}
	example := `Example: {"remember":[{"op":"add","scope":"dot","kind":"note","text":"example text"}]} Real: {"remember": [],}`
	if ops, err := ParseMemoryOps(example); !errors.Is(err, ErrMalformed) {
		t.Errorf("example object then a broken real one: ops = %+v, err = %v, want ErrMalformed", ops, err)
	}
}
