package diff

import (
	"strings"
	"testing"

	"herdr-dots/internal/patchstore"
)

const workflowPatch = "diff --git a/.github/workflows/x.yml b/.github/workflows/x.yml\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/.github/workflows/x.yml\n@@ -0,0 +1 @@\n+on: push\n" +
	"diff --git a/src/a.go b/src/a.go\nindex 1111111..2222222 100644\n--- a/src/a.go\n+++ b/src/a.go\n@@ -1 +1 @@\n-a\n+b\n"

func TestAgentConfigFilesAreFlaggedInTheListAndTheConfirm(t *testing.T) {
	meta := patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "2 files", Paths: []string{".github/workflows/x.yml", "src/a.go"}}
	m := sized(New([]byte(workflowPatch), meta, "7", &recCaller{}), 140, 30)
	v := m.View()
	var tagged, plain bool
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "x.yml") && strings.Contains(l, "[agent/CI config]") {
			tagged = true
		}
		if strings.Contains(l, "src/a.go") && !strings.Contains(l, "[agent/CI config]") {
			plain = true
		}
	}
	if !tagged || !plain {
		t.Fatalf("list tags (workflow tagged %v, source untagged %v):\n%s", tagged, plain, v)
	}
	if v := send(m, "A").View(); !strings.Contains(v, "Includes: ") || !strings.Contains(v, "1 agent/CI config") {
		t.Fatalf("confirm lacks the agent/CI config warning:\n%s", v)
	}
}
