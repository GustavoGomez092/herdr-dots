package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

// U22 (post-rc hardening, the developer's decision): deleting a dot asks
// "Also delete its memory in all projects? (y/N)" and the default KEEPS the
// memory. Only y deletes it; enter, n, esc and any other key keep it.
func TestDeleteDotMemoryQuestionDefaultsToKeep(t *testing.T) {
	cases := []struct {
		key  string
		keep bool
	}{
		{"enter", true},
		{"n", true},
		{"esc", true},
		{"x", true},
		{" ", true},
		{"y", false},
		{"Y", false},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			m, _, dir, st := memSetup(t)
			d, _ := dots.FromTemplate("backend")
			_ = dots.Save(dir, d, true)
			addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Handlers return problem+json."})
			m = press(m, "D", "y")
			v := m.View()
			if !strings.Contains(v, "Also delete its memory in all projects? (y/N)") {
				t.Fatalf("memory question must default to no:\n%s", v)
			}
			if strings.Contains(v, "y/enter") || strings.Contains(v, "(Y/n)") {
				t.Fatalf("enter must not be offered as delete:\n%s", v)
			}
			m = press(m, c.key)
			es, _ := st.List(memPID, memory.Filter{Dot: "backend"})
			if c.keep {
				if len(es) != 1 {
					t.Fatalf("%q must keep the memory: %+v", c.key, es)
				}
				if v := m.View(); !strings.Contains(v, "DOTS LIBRARY") || !strings.Contains(v, "memory kept") {
					t.Fatalf("%q returns to the library saying memory kept:\n%s", c.key, v)
				}
				return
			}
			if len(es) != 0 {
				t.Fatalf("%q must delete the memory: %+v", c.key, es)
			}
			if !strings.Contains(m.View(), "deleted Backend and its memory") {
				t.Fatalf("status:\n%s", m.View())
			}
		})
	}
}
