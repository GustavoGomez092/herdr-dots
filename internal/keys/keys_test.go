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
	if len(add) != 1 || add[0].Key != "prefix+shift+s" {
		t.Fatalf("add %v", add)
	}
}

func TestPlanTreatsHerdrBuiltinsAsTaken(t *testing.T) {
	_, _, conflicts := Plan([]Binding{{Key: "prefix+s", Action: "herdr-dots.summon"}, {Key: "prefix+shift+d", Action: "x"}}, map[string]string{})
	if len(conflicts) != 2 {
		t.Fatalf("built-ins must conflict: %v", conflicts)
	}
}

func TestBlockIsValidTOMLAndAppendBacksUp(t *testing.T) {
	block := Block(Defaults())
	var parsed struct {
		Keys struct {
			Command []map[string]string `toml:"command"`
		} `toml:"keys"`
	}
	if _, err := toml.Decode(block, &parsed); err != nil || len(parsed.Keys.Command) != 3 || parsed.Keys.Command[0]["type"] != "plugin_action" {
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
