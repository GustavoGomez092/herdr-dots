package keys

import (
	"strings"
	"testing"
)

func TestRemoveBlockKeepsEverythingElseByteForByte(t *testing.T) {
	for _, c := range []struct{ name, user, want string }{
		{"ends in another table", "# my config\n" + userConfig + "\n# a comment the user wrote\n[ui]\ntheme = \"dark\" # inline\n", ""},
		{"ends in someone's binding", userConfig, ""},
		{"empty config", "", ""},
		// setup-keys's leading newline ended the user's last line; it stays.
		{"no newline at the very end", "[ui]\ntheme = \"dark\"", "[ui]\ntheme = \"dark\"\n"},
	} {
		want := c.want
		if want == "" {
			want = c.user
		}
		out, removed, left, err := RemoveBlock(c.user + Block(Defaults()))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if removed != len(Defaults()) || left != 0 { // manage, toggle-pane, summon, council
			t.Fatalf("%s: removed %d left %d", c.name, removed, left)
		}
		if out != want {
			t.Fatalf("%s: other bytes changed:\n--- want\n%q\n--- got\n%q", c.name, want, out)
		}
	}
}

func TestRemoveBlockFindsHandWrittenBindingsInTheMiddle(t *testing.T) {
	in := "[keys]\nprefix = \"ctrl+a\"\n\n[[keys.command]]\nkey = \"prefix+d\"\ncommand = 'herdr-dots.manage'\ntype = \"plugin_action\"\n\n[[keys.command]]\nkey = \"prefix+up\"\ntype = \"plugin_action\"\ncommand = \"cloudmanic.herdr-plus.projects\"\n"
	out, removed, left, err := RemoveBlock(in)
	if err != nil || removed != 1 || left != 0 {
		t.Fatalf("removed %d left %d err %v", removed, left, err)
	}
	if strings.Contains(out, "herdr-dots") || !strings.Contains(out, "cloudmanic.herdr-plus.projects") || !strings.Contains(out, "prefix = \"ctrl+a\"") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestRemoveBlockCountsBindingsItCannotRemove(t *testing.T) {
	in := "[keys]\ncommand = [{ key = \"prefix+d\", type = \"plugin_action\", command = \"herdr-dots.manage\" }]\n"
	out, removed, left, err := RemoveBlock(in)
	if err != nil || removed != 0 || left != 1 || out != in {
		t.Fatalf("removed %d left %d err %v out %q", removed, left, err, out)
	}
}

func TestRemoveBlockWithNothingToRemoveIsANoOp(t *testing.T) {
	out, removed, left, err := RemoveBlock(userConfig)
	if err != nil || removed != 0 || left != 0 || out != userConfig {
		t.Fatalf("removed %d left %d err %v", removed, left, err)
	}
}
