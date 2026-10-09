package dotpack

import (
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

// FuzzDotpackParse feeds arbitrary bytes to the pack parser, whose input
// comes from other people (a file or an https download). It must return
// quickly (the TOML bomb pre-scan), and a pack it accepts must only carry
// what may travel; preparing it clamps every risky capability, and clamping
// the prepared dot again changes nothing.
func FuzzDotpackParse(f *testing.F) {
	d := dots.New("Security")
	d.Purpose = "Flag secrets."
	if b, err := Export(d, nil, ExportOptions{Note: "seed", Version: "1.0.0-rc.1", Now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}); err == nil {
		f.Add(string(b))
	}
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"x\"\npurpose = \"p\"\n")
	f.Add("[pack]\nformat = 99\n")
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"W\"\npurpose = \"p\"\n[dot.capabilities]\nwrite = true\n[dot.delivery]\nmode = \"inject\"\n")
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"M\"\npurpose = \"p\"\n[[memory]]\nscope = \"global\"\nkind = \"note\"\ntext = \"x\"\n")
	f.Add("a = " + strings.Repeat("[", 4000))
	f.Fuzz(func(t *testing.T, data string) {
		p, err := Parse([]byte(data))
		if err != nil {
			return
		}
		if p.Dot.Origin != dots.OriginImport {
			t.Fatalf("a parsed pack's dot has origin %q", p.Dot.Origin)
		}
		for _, e := range p.Memory {
			if (e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject) || !slices.Contains(memory.ImportKinds, e.Kind) {
				t.Fatalf("pack memory that may not travel: %+v", e)
			}
		}
		prepared, _, err := Prepare(p, nil)
		if err != nil {
			return
		}
		if prepared.Origin != dots.OriginImport || prepared.Capabilities.Write || prepared.Delivery.Mode == dots.ModeInject {
			t.Fatalf("Prepare kept a risky capability: %+v", prepared)
		}
		if _, again := dots.ClampDot(prepared, nil); slices.ContainsFunc(again, func(c dots.Clamp) bool { return !c.Fact }) {
			t.Fatalf("clamping a prepared dot changed it again: %+v", again)
		}
	})
}
