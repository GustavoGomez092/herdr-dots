package mcpinstall

import (
	"slices"
	"strings"
	"testing"
)

// FuzzCodexBlock edits arbitrary Codex config files the way setup-mcp edits
// the developer's ~/.codex/config.toml: installing twice is a no-op, and
// removing after an install leaves no marker line of ours (and, for a file
// without the server, gives the file back byte for byte). (The developer's
// own text may quote the marker inside a longer line, or even hold a whole
// stray marker line outside our block; those lines are theirs and stay:
// crashers a763424096dd21b2 and 816ef7fa033511c9. So a removal may keep the
// marker lines the original file had, never add one.)
func FuzzCodexBlock(f *testing.F) {
	f.Add("")
	f.Add("model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n")
	f.Add(CodexBlock("/x/bin/herdr-dots", "/c", "/s"))
	f.Add("[mcp_servers.herdr-dots]\ncommand = \"old\"\n")
	f.Add(BlockBegin + "\nbroken")
	f.Fuzz(func(t *testing.T, before string) {
		after, err := UpsertCodexBlock(before, "/x/bin/herdr-dots", "/c", "/s")
		if err != nil {
			return // refused as unsafe: nothing was written
		}
		again, err := UpsertCodexBlock(after, "/x/bin/herdr-dots", "/c", "/s")
		if err != nil || again != after {
			t.Fatalf("installing twice is not a no-op (%v):\n%s\n---\n%s", err, after, again)
		}
		removed, found, err := RemoveCodexBlock(after)
		if err != nil || !found {
			t.Fatalf("remove after install: found=%v err=%v\n%s", found, err, removed)
		}
		markers := func(s string) (n int) {
			for _, l := range strings.Split(s, "\n") {
				if m := strings.TrimSpace(l); m == BlockBegin || m == BlockEnd {
					n++
				}
			}
			return n
		}
		if markers(removed) > markers(before) {
			t.Fatalf("remove after install left a marker line:\n%s", removed)
		}
		// A file with neither our block nor our table got the block appended,
		// so removing it must give the developer's file back byte for byte.
		old, _ := parseCodex(before) // Upsert parsed it already
		hasBegin := slices.ContainsFunc(strings.Split(before, "\n"), func(l string) bool { return strings.TrimSpace(l) == BlockBegin })
		if !hasBegin && !hasServer(old) && removed != before {
			t.Fatalf("install then remove changed a file without the server:\n%q\n---\n%q", before, removed)
		}
	})
}
