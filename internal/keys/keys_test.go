package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const userConfig = `onboarding = false
[keys]
prefix = "ctrl+a"
settings = "prefix+t"

[[keys.command]]
key = "prefix+up"
type = "plugin_action"
command = "cloudmanic.herdr-plus.projects"

[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "someone.else.thing"
`

func TestConfigPath(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if p := ConfigPath(get, "/h"); p != "/h/.config/herdr/config.toml" {
		t.Fatal(p)
	}
	env["XDG_CONFIG_HOME"] = "/x"
	if p := ConfigPath(get, "/h"); p != "/x/herdr/config.toml" {
		t.Fatal(p)
	}
	env["HERDR_CONFIG_PATH"] = "/custom.toml"
	if p := ConfigPath(get, "/h"); p != "/custom.toml" {
		t.Fatal(p)
	}
}

func TestExistingReadsCommandsAndKeyTable(t *testing.T) {
	ex, err := Existing([]byte(userConfig))
	if err != nil {
		t.Fatal(err)
	}
	if ex["prefix+up"] != "cloudmanic.herdr-plus.projects" || ex["prefix+t"] != "herdr: settings" || ex["prefix+a"] != "someone.else.thing" {
		t.Fatalf("existing %v", ex)
	}
	if _, ok := ex["ctrl+a"]; ok {
		t.Fatal("the prefix itself is not a binding")
	}
}

func TestPlanSkipsConflictsAndAlreadyBound(t *testing.T) {
	ex, _ := Existing([]byte(userConfig + "\n[[keys.command]]\nkey = \"prefix+d\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n"))
	add, already, conflicts := Plan(Defaults(), ex)
	if len(already) != 1 || already[0].Key != "prefix+d" {
		t.Fatalf("already %v", already)
	}
	if len(conflicts) != 1 || conflicts[0].Binding.Key != "prefix+a" || conflicts[0].With != "someone.else.thing" {
		t.Fatalf("conflicts %v", conflicts)
	}
	if len(add) != 2 || add[0].Key != "prefix+shift+s" || add[1].Key != "prefix+shift+c" {
		t.Fatalf("add %v", add)
	}
}

func TestPlanTreatsHerdrBuiltinsAsTaken(t *testing.T) {
	_, _, conflicts := Plan([]Binding{{Key: "prefix+s", Action: "herdr-dots.summon"}, {Key: "prefix+shift+d", Action: "x"}}, map[string]string{})
	if len(conflicts) != 2 {
		t.Fatalf("built-ins must conflict: %v", conflicts)
	}
}

// An inline `keys.command = [...]` array can't be extended by [[keys.command]]
// tables: appending would break the user's Herdr config.
func TestAppendRefusesWhenResultWouldNotParse(t *testing.T) {
	const inline = "[keys]\nprefix = \"ctrl+a\"\ncommand = [{ key = \"prefix+up\", type = \"plugin_action\", command = \"x.y\" }]\n"
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	os.WriteFile(p, []byte(inline), 0o640)
	if _, err := Existing([]byte(inline)); err != nil {
		t.Fatalf("the inline form is valid TOML on its own: %v", err)
	}
	if _, err := Append(p, Defaults()[2:], time.Now()); err == nil {
		t.Fatal("Append must refuse when the result is not valid TOML")
	}
	now, _ := os.ReadFile(p)
	if string(now) != inline {
		t.Fatalf("file must be untouched, got:\n%s", now)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed: %v", st.Mode().Perm())
	}
}

func TestAppendPreservesFileModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles-config.toml")
	os.WriteFile(real, []byte(userConfig), 0o640)
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Append(link, Defaults()[2:], time.Now()); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a symlinked config must stay a symlink")
	}
	st, _ := os.Stat(real)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640", st.Mode().Perm())
	}
	if data, _ := os.ReadFile(real); !strings.Contains(string(data), "prefix+shift+s") {
		t.Fatalf("not appended:\n%s", data)
	}
}

func TestBlockIsValidTOMLAndAppendBacksUp(t *testing.T) {
	block := Block(Defaults())
	var parsed struct {
		Keys struct {
			Command []map[string]string `toml:"command"`
		} `toml:"keys"`
	}
	if _, err := toml.Decode(block, &parsed); err != nil || len(parsed.Keys.Command) != 4 || parsed.Keys.Command[0]["type"] != "plugin_action" {
		t.Fatalf("block:\n%s\nerr %v", block, err)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(userConfig), 0o600)
	backup, err := Append(p, Defaults()[2:], time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(backup)
	if string(orig) != userConfig {
		t.Fatal("backup must hold the original")
	}
	now, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(now), userConfig) || !strings.Contains(string(now), `key = "prefix+shift+s"`) {
		t.Fatalf("appended:\n%s", now)
	}
	if _, err := Existing(now); err != nil {
		t.Fatalf("result must stay valid TOML: %v", err)
	}
}

func TestCouncilKeyIsFreeUnlessTheUserBoundIt(t *testing.T) {
	council := Defaults()[3]
	if council.Key != "prefix+shift+c" || council.Action != "herdr-dots.council" {
		t.Fatalf("council binding %+v", council)
	}
	add, _, conflicts := Plan([]Binding{council}, map[string]string{})
	if len(add) != 1 || len(conflicts) != 0 {
		t.Fatalf("prefix+shift+c is not a Herdr built-in: add %v conflicts %v", add, conflicts)
	}
	_, _, conflicts = Plan([]Binding{council}, map[string]string{"prefix+shift+c": "someone.else.copy"})
	if len(conflicts) != 1 || conflicts[0].With != "someone.else.copy" {
		t.Fatalf("a user binding wins: %v", conflicts)
	}
}
