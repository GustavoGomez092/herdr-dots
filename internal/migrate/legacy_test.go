package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

const legacyDot = `name = "Security"
glyph = "◆"
color = "red"
purpose = """
Flag authentication flaws and secrets in code."""

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

// legacy holds files exactly as each released 0.x version wrote them, by
// root ("config", "state", "session") and path. Each version keeps the files
// of the versions before it.
var legacy = map[string]map[string]string{
	"0.1": {
		"config/config.toml":        "default_backend = \"claude-cli\"\ndefault_model = \"sonnet\"\ninject_enabled = true\n",
		"config/dots/security.toml": legacyDot,
		"session/attachments.json":  `{"w1:p1":{"pane_id":"w1:p1","agent":"claude","session_kind":"id","session_value":"abc","cwd":"/repo","source":"transcript","force_scrollback":false,"dots":[{"slug":"security","last_seen_turn":4,"paused":false,"cooldown_left":0,"failures":0}]}}`,
		"session/queue.json":        `{"next":2,"cards":[{"id":"c1","dot":"security","dot_name":"Security","glyph":"◆","pane_id":"w1:p1","turn":4,"severity":"warn","body":"Token logged in auth.go:12.","mode":"queue","approved":false,"status":"pending","created_at":"2026-10-01T10:00:00Z"}]}`,
		"session/usage.json":        `{"w1:p1":{"security":{"calls":3,"input_tokens":1200,"output_tokens":90}}}`,
		"session/feed.jsonl":        `{"time":"2026-10-01T10:00:00Z","pane_id":"w1:p1","dot":"security","turn":4,"outcome":"flagged","detail":"warn"}` + "\n",
	},
	"0.2": {
		"config/config.toml":                                 "default_backend = \"claude-cli\"\nmemory_enabled = true\nmemory_retention_days = 180\n",
		"state/memory/g-0123456789abcdef/meta.json":          `{"version":1,"id":"g-0123456789abcdef","name":"repo","roots":["/repo"],"created_at":"2026-10-02T10:00:00Z"}`,
		"state/memory/g-0123456789abcdef/project.json":       `{"version":1,"next":1,"entries":[{"id":"m1","scope":"project","kind":"decision","text":"Auth tokens live in httpOnly cookies.","status":"active","source":"user","pinned":true,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{}}]}`,
		"state/memory/g-0123456789abcdef/dots/security.json": `{"version":1,"next":1,"entries":[{"id":"m1","scope":"dot","dot":"security","kind":"dismissed","text":"CSRF on /admin is intentionally off.","status":"active","source":"outcome","pinned":false,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{"card":"c1"}}]}`,
		"state/memory/g-0123456789abcdef/ledger.jsonl":       `{"at":"2026-10-02T10:00:00Z","dot":"security","card":"c1","outcome":"dismissed","body":"CSRF on /admin."}` + "\n",
		"state/memory/_global/dots/security.json":            `{"version":1,"next":1,"entries":[{"id":"m1","scope":"global","dot":"security","kind":"preference","text":"Keep findings to two sentences.","status":"active","source":"user","pinned":false,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{}}]}`,
		"session/attachments.json":                           `{"w1:p1":{"pane_id":"w1:p1","agent":"claude","session_kind":"id","session_value":"abc","cwd":"/repo","source":"transcript","force_scrollback":false,"dots":[{"slug":"security","last_seen_turn":4,"paused":false,"cooldown_left":0,"failures":0,"memory_seen_turn":4,"memory_passes":1}],"project_id":"g-0123456789abcdef","project_name":"repo"}}`,
	},
	"0.3": {
		"config/config.toml": "default_backend = \"claude-cli\"\n\n[council]\nrounds = 2\nmax_members = 4\nmerge = \"chair\"\n",
		"session/queue.json": `{"next":3,"cards":[{"id":"c2","dot":"council","dot_name":"Council","glyph":"⊕","pane_id":"w1:p1","turn":5,"severity":"critical","body":"1. Token logged (Security).","mode":"queue","approved":false,"status":"pending","created_at":"2026-10-03T10:00:00Z","kind":"council","question":"Is this safe?","parts":[{"dot":"security","dot_name":"Security","glyph":"◆","severity":"critical","body":"Token logged."},{"dot":"frontend","dot_name":"Frontend","glyph":"▲","failed":true,"body":"timed out"}]}]}`,
	},
}

// legacyTree writes version v and every version before it into a temp tree.
func legacyTree(t *testing.T, v string) Roots {
	t.Helper()
	base := t.TempDir()
	r := Roots{Config: filepath.Join(base, "config"), State: filepath.Join(base, "state"), Session: filepath.Join(base, "session")}
	var versions []string
	for k := range legacy {
		if k <= v {
			versions = append(versions, k)
		}
	}
	sort.Strings(versions)
	for _, ver := range versions {
		for rel, body := range legacy[ver] {
			writeFile(t, filepath.Join(base, filepath.FromSlash(rel)), body)
		}
	}
	return r
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasSuffix(p, ".lock") {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return nil
	})
	return out
}

// Every released format is already current in 1.0: Run rewrites nothing, and
// every file still loads through its own package afterwards.
func TestEveryReleasedVersionIsCurrentAndStillLoads(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	for _, v := range []string{"0.1", "0.2", "0.3"} {
		t.Run(v, func(t *testing.T) {
			r := legacyTree(t, v)
			before := snapshotTree(t, filepath.Dir(r.Config))
			rep, err := Default().Run(r, Options{})
			if err != nil || !rep.Empty() || len(rep.Backups) != 0 {
				t.Fatalf("report %+v %v", rep, err)
			}
			if after := snapshotTree(t, filepath.Dir(r.Config)); !reflect.DeepEqual(before, after) {
				t.Fatal("Run changed a released file")
			}
			if _, err := config.Load(filepath.Join(r.Config, "config.toml")); err != nil {
				t.Fatalf("config: %v", err)
			}
			if ds, bad := dots.LoadAll(filepath.Join(r.Config, "dots")); len(ds) != 1 || len(bad) != 0 {
				t.Fatalf("dots: %v %v", ds, bad)
			}
			q, err := queue.Open(filepath.Join(r.Session, "queue.json"))
			if err != nil || len(q.All()) != 1 {
				t.Fatalf("queue: %v", err)
			}
			if a, err := state.OpenAttachments(filepath.Join(r.Session, "attachments.json")); err != nil || len(a.List()) != 1 {
				t.Fatalf("attachments: %v", err)
			}
			if u, err := state.OpenUsage(filepath.Join(r.Session, "usage.json")); err != nil || u.Calls("w1:p1", "security") != 3 {
				t.Fatalf("usage: %v", err)
			}
			if f, err := state.OpenFeed(filepath.Join(r.Session, "feed.jsonl")); err != nil || len(f.Recent("w1:p1", 5)) != 1 {
				t.Fatalf("feed: %v", err)
			}
			if v >= "0.2" {
				st := memory.Open(filepath.Join(r.State, "memory"), memory.DefaultLimits())
				es, err := st.List("g-0123456789abcdef", memory.Filter{})
				if err != nil || len(es) < 2 {
					t.Fatalf("memory: %d entries, %v", len(es), err)
				}
			}
		})
	}
}

// A store a future version wrote stays readable and is never overwritten.
func TestFutureQueueIsReadButNeverRewritten(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r := legacyTree(t, "0.3")
	qf := filepath.Join(r.Session, "queue.json")
	future := `{"version":2,"next":3,"cards":[],"lanes":["a"]}`
	writeFile(t, qf, future)
	rep, err := Default().Run(r, Options{})
	if err != nil || len(rep.ReadOnly) != 1 || rep.ReadOnly[0] != qf {
		t.Fatalf("report %+v %v", rep, err)
	}
	q, err := queue.Open(qf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Add(queue.Card{Dot: "security", PaneID: "w1:p1", Body: "x", Status: queue.StatusPending}); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a write to the future queue must fail with ErrNewerVersion, got %v", err)
	}
	if readFile(t, qf) != future {
		t.Fatal("future queue changed")
	}
}

// Every file a paths.Paths method names is either a kind or deliberately
// unversioned. A phase that adds a file must classify it here.
func TestEveryPathsFileIsClassified(t *testing.T) {
	kinds := map[string]bool{}
	for _, k := range Default().Kinds {
		kinds[k.Name] = true
	}
	for name, kind := range KindPaths {
		if !kinds[kind] {
			t.Errorf("KindPaths[%s] = %q, which is not a kind", name, kind)
		}
	}
	typ := reflect.TypeOf(paths.Paths{})
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.String {
			continue // only path getters: func (Paths) X() string
		}
		if _, ok := KindPaths[m.Name]; ok {
			continue
		}
		if _, ok := Unversioned[m.Name]; ok {
			continue
		}
		t.Errorf("paths.Paths.%s is neither a migrate kind (KindPaths) nor listed in Unversioned", m.Name)
	}
}

// Every JSON kind outside a session directory can be upgraded by another
// process while this one runs (the config and state dirs are shared by every
// Herdr session), after Run has checked it. Its own loader must refuse a
// newer version, so it is never silently downgraded.
func TestSharedJSONKindsAreSelfGuarded(t *testing.T) {
	for _, k := range Default().Kinds {
		if k.Root != SessionRoot && k.Format == JSON && !k.SelfGuarded {
			t.Errorf("kind %q is shared JSON state: its loader must refuse newer versions (SelfGuarded)", k.Name)
		}
	}
}
