package keys

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestRemoveBlockKeepsCommentsBlankLinesAndCRLFAroundTheBlock(t *testing.T) {
	cases := map[string]struct{ before, after string }{
		"comment after the block":      {"[ui]\ntheme = \"dark\"\n", "\n# my notes\n"},
		"blank lines before the block": {"[ui]\ntheme = \"dark\"\n\n\n\n", ""},
		"blank lines after the block":  {"[ui]\n", "\n\n\n"},
		"crlf everywhere":              {"[ui]\r\ntheme = \"dark\"\r\n\r\n# c\r\n", "\r\n# tail\r\n"},
		"comment above own binding":    {"# mine\n[[keys.command]]\nkey = \"prefix+x\"\ntype = \"plugin_action\"\ncommand = \"other.x\"\n", "\n# above\n[[keys.command]]\nkey = \"prefix+y\"\ntype = \"plugin_action\"\ncommand = \"other.y\"\n"},
		"stray header-like comment":    {"# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n[ui]\na = 1\n", ""},
	}
	for name, c := range cases {
		out, removed, left, err := RemoveBlock(c.before + Block(Defaults()) + c.after)
		if err != nil || removed != len(Defaults()) || left != 0 {
			t.Fatalf("%s: removed %d left %d err %v", name, removed, left, err)
		}
		if out != c.before+c.after {
			t.Fatalf("%s: bytes changed:\n want %q\n got  %q", name, c.before+c.after, out)
		}
	}
}

func TestRemoveBlockFallbackKeepsTrailingCommentsAndBlankLines(t *testing.T) {
	in := "[ui]\na = 1\n\n# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n\n[[keys.command]]\nkey = \"prefix+d\"\ncommand = \"herdr-dots.manage\"\ntype = \"plugin_action\"\n\n# trailing note\n\n[[keys.command]]\nkey = \"prefix+z\"\ntype = \"plugin_action\"\ncommand = \"other.z\"\n"
	want := "[ui]\na = 1\n\n# trailing note\n\n[[keys.command]]\nkey = \"prefix+z\"\ntype = \"plugin_action\"\ncommand = \"other.z\"\n"
	out, removed, _, err := RemoveBlock(in)
	if err != nil || removed != 1 || out != want {
		t.Fatalf("removed %d err %v\n%q", removed, err, out)
	}
}

func TestRemoveBlockExactBlockPropertyKeepsUserTextByteForByte(t *testing.T) {
	pieces := []string{"# a comment\n", "\n", "\r\n", "# crlf comment\r\n", "x = 1\n", "y = \"s\" # c\r\n",
		"[[other]]\nk = 1\n", "[[keys.command]]\nkey = \"prefix+q\"\ntype = \"plugin_action\"\ncommand = \"x.y\"\n",
		"# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n"}
	rng := rand.New(rand.NewSource(7))
	tried := 0
	for i := 0; i < 3000; i++ {
		var user strings.Builder
		for n := rng.Intn(8); n > 0; n-- {
			user.WriteString(pieces[rng.Intn(len(pieces))])
		}
		u := user.String()
		if _, err := toml.Decode(u, new(map[string]any)); err != nil {
			continue // not a config a developer could have
		}
		tried++
		after := ""
		if rng.Intn(2) == 0 {
			after = "\n# tail\n"
		}
		out, removed, left, err := RemoveBlock(u + Block(Defaults()) + after)
		if err != nil || removed != len(Defaults()) || left != 0 || out != u+after {
			t.Fatalf("iteration %d: removed %d left %d err %v\nuser %q\ngot  %q", i, removed, left, err, u, out)
		}
	}
	if tried < 500 {
		t.Fatalf("only %d valid inputs", tried)
	}
}

func TestRemoveBlockKeepsEverythingElseByteForByte(t *testing.T) {
	for _, c := range []struct{ name, user, want string }{
		{"ends in another table", "# my config\n" + userConfig + "\n# a comment the user wrote\n[ui]\ntheme = \"dark\" # inline\n", ""},
		{"ends in someone's binding", userConfig, ""},
		{"empty config", "", ""},
		// The block's own leading newline goes with it: byte for byte.
		{"no newline at the very end", "[ui]\ntheme = \"dark\"", ""},
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
