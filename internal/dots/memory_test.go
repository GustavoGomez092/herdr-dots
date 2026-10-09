package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHasMemoryDefaults(t *testing.T) {
	if m := New("Security").Memory; m != DefaultMemory() || !m.Enabled || !m.Learn || !m.ReadProject || m.EveryTurns != 5 || m.MaxPassesPerSession != 10 || m.PromptChars != 6000 {
		t.Fatalf("defaults %+v", m)
	}
}

const v1Dot = `name = "Security"
glyph = "◆"
color = "red"
purpose = "Find auth bugs."

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
`

func TestV1DotFileWithoutMemoryGetsDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot), 0o600)
	d, err := Load(dir, "security")
	if err != nil || d.Memory != DefaultMemory() {
		t.Fatalf("%+v %v", d.Memory, err)
	}
}

func TestPartialMemoryTableKeepsOtherDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot+"\n[memory]\nlearn = false\nevery_turns = 7\n"), 0o600)
	d, err := Load(dir, "security")
	if err != nil {
		t.Fatal(err)
	}
	if d.Memory.Learn || d.Memory.EveryTurns != 7 || !d.Memory.Enabled || d.Memory.PromptChars != 6000 {
		t.Fatalf("%+v", d.Memory)
	}
}

func TestMemoryValidation(t *testing.T) {
	cases := []struct {
		set  func(*MemorySpec)
		want string
	}{
		{func(m *MemorySpec) { m.EveryTurns = 0 }, "every_turns"},
		{func(m *MemorySpec) { m.EveryTurns = 51 }, "every_turns"},
		{func(m *MemorySpec) { m.MaxPassesPerSession = -1 }, "max_passes_per_session"},
		{func(m *MemorySpec) { m.MaxPassesPerSession = 201 }, "max_passes_per_session"},
		{func(m *MemorySpec) { m.PromptChars = -1 }, "prompt_chars"},
		{func(m *MemorySpec) { m.PromptChars = 20001 }, "prompt_chars"},
	}
	for _, c := range cases {
		d := New("Security")
		d.Purpose = "p"
		c.set(&d.Memory)
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want an error about %s, got %v", c.want, err)
		}
	}
	d := New("Security")
	d.Purpose = "p"
	d.Memory.PromptChars, d.Memory.MaxPassesPerSession = 0, 0
	if err := d.Validate(); err != nil {
		t.Fatalf("0 prompt_chars and 0 passes are valid: %v", err)
	}
}

func TestSaveRoundTripsMemory(t *testing.T) {
	dir := t.TempDir()
	d := valid("Security")
	d.Memory.Learn, d.Memory.EveryTurns, d.Memory.PromptChars = false, 9, 4000
	if err := Save(dir, d, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "security.toml"))
	if !strings.Contains(string(raw), "[memory]") {
		t.Fatalf("no [memory] table:\n%s", raw)
	}
	got, err := Load(dir, "security")
	if err != nil || got.Memory != d.Memory || got.Purpose != d.Purpose {
		t.Fatalf("%+v %v", got.Memory, err)
	}
}

func TestTemplatesGetMemoryDefaults(t *testing.T) {
	for _, d := range Templates() {
		if d.Memory != DefaultMemory() {
			t.Errorf("template %s memory %+v", d.Slug, d.Memory)
		}
	}
}
