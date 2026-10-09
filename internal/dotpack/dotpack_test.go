package dotpack

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

var dpNow = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

func dpDot() dots.Dot {
	d := dots.New("Security")
	d.Glyph, d.Color = "◆", "red"
	d.Purpose = "You review security.\nSay \"why\" in one line.\n[not a table]"
	d.Trigger.FileGlobs = []string{"src/**"}
	d.Origin = dots.OriginTemplate
	return d
}

func dpMem() []memory.Entry {
	return []memory.Entry{
		{Scope: memory.ScopeDot, Kind: memory.KindConvention, Text: "Prefer zod schemas at API boundaries.", Paths: []string{"src/api/**"}},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."},
	}
}

func clampNames(cs []dots.Clamp) string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Capability)
	}
	return strings.Join(names, ",")
}

func TestExportParseRoundTrip(t *testing.T) {
	d := dpDot()
	data, err := Export(d, dpMem(), ExportOptions{Note: "tuned for Next.js", Version: "0.9.0", Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"[pack]", "format = 1", "herdr_dots_version = \"0.9.0\"", "[dot]", "purpose = \"\"\"\n", "[dot.backend]", "[[memory]]"} {
		if !strings.Contains(s, want) {
			t.Errorf("pack missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "origin") {
		t.Fatalf("origin travels:\n%s", s)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Note != "tuned for Next.js" || !p.Meta.ExportedAt.Equal(dpNow) || len(p.Memory) != 2 || p.Memory[0].Paths[0] != "src/api/**" {
		t.Fatalf("meta/memory %+v", p)
	}
	got, cs, err := Prepare(p, dots.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := d
	want.Origin = dots.OriginImport
	// Someone else's dot always gets the shared dollar cap (Task 2's budget
	// clamp, which can't be kept), and the preview facts (kind, model).
	want.Budget.MaxUSDPerSession = dots.SharedMaxUSDPerSession
	if !reflect.DeepEqual(got, want) || clampNames(cs) != "budget,backend,backend" {
		t.Fatalf("round trip:\n got %+v\nwant %+v\nclamps %v", got, want, cs)
	}
}

func TestPrepareClampsAndListsEachCapability(t *testing.T) {
	d := dpDot()
	d.Delivery.Mode = dots.ModeInject
	d.Capabilities.Write = true
	d.Backend.Args = []string{"--dangerously-skip-permissions"}
	data, err := Export(d, nil, ExportOptions{Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Parse(data)
	got, cs, err := Prepare(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Delivery.Mode != dots.ModeQueue || got.Capabilities.Write || got.Capabilities.MCP || len(got.Backend.Args) != 0 {
		t.Fatalf("not clamped: %+v", got)
	}
	if names := clampNames(cs); names != "inject,write,mcp,args,budget,backend,backend" {
		t.Fatalf("clamps %v", names)
	}
	// The preview facts: backend kind and the effective model.
	if cs[5].From != "claude-cli" || cs[6].From != "sonnet" || !cs[5].Fact || !cs[6].Fact {
		t.Fatalf("facts %+v %+v", cs[5], cs[6])
	}
	kept, _, _ := Prepare(p, []string{dots.CapInject})
	if kept.Delivery.Mode != dots.ModeInject || kept.Capabilities.Write {
		t.Fatalf("--keep inject restores only inject: %+v", kept)
	}
	if _, _, err := Prepare(p, []string{"shell"}); err == nil {
		t.Fatal("unknown --keep accepted")
	}
}

func TestParseAndPrepareSetOriginImport(t *testing.T) {
	good, err := Export(dpDot(), nil, ExportOptions{Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(good), "[dot]\n", "[dot]\norigin = \"template\"\n", 1)
	p, err := Parse([]byte(forged))
	if err != nil {
		t.Fatal(err)
	}
	if p.Dot.Origin != dots.OriginImport {
		t.Fatalf("parsed origin %q", p.Dot.Origin)
	}
	p.Dot.Origin = dots.OriginTemplate // a caller that fiddles with the pack
	got, _, err := Prepare(p, nil)
	if err != nil || got.Origin != dots.OriginImport {
		t.Fatalf("prepared origin %q %v", got.Origin, err)
	}
}

func TestPrepareRunsFullValidate(t *testing.T) {
	d := dpDot()
	d.Name = "Sec\x1b[31murity"
	data, err := Export(d, nil, ExportOptions{Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Prepare(p, nil); err == nil {
		t.Fatal("a control character in the name was accepted")
	}
	d = dpDot()
	d.Schedule.Cron = "every day please"
	data, _ = Export(d, nil, ExportOptions{Now: dpNow})
	p, _ = Parse(data)
	if _, _, err := Prepare(p, []string{dots.CapSchedule}); err == nil {
		t.Fatal("a kept bad cron was accepted")
	}
}

func TestExportRefusesSecretsUnlessForced(t *testing.T) {
	mem := append(dpMem(), memory.Entry{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789"})
	_, err := Export(dpDot(), mem, ExportOptions{})
	var se *SecretError
	if !errors.As(err, &se) || !errors.Is(err, ErrSecretInExport) || strings.Join(se.Where, ",") != "memory 3" {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Fatal("the error quoted the secret")
	}
	d := dpDot()
	d.Purpose += "\nUse key sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := Export(d, nil, ExportOptions{}); !errors.As(err, &se) || se.Where[0] != "purpose" {
		t.Fatalf("purpose secret: %v", err)
	}
	if _, err := Export(dpDot(), mem, ExportOptions{Force: true}); err != nil {
		t.Fatalf("--force: %v", err)
	}
	// Phase 8's free-text fields travel in the pack, so they are scanned too.
	d = dpDot()
	d.Schedule.Task = "Post to https://hooks.example/x with xoxb-1234567890-abcdefghij"
	d.Research.Topics = []string{"AKIAIOSFODNN7EXAMPLE rotation"}
	if _, err := Export(d, nil, ExportOptions{}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "research topics,schedule task" {
		t.Fatalf("proactive text secrets: %v", err)
	}
}

func TestExportForceDropsSecretMemoryButNeverShipsIt(t *testing.T) {
	mem := append(dpMem(), memory.Entry{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789"})
	data, err := Export(dpDot(), mem, ExportOptions{Force: true, Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ghp_") {
		t.Fatalf("--force shipped the secret:\n%s", data)
	}
	p, err := Parse(data)
	if err != nil || len(p.Memory) != 2 {
		t.Fatalf("memory %+v %v", p.Memory, err)
	}
}

func TestExportRefusesDotSecretsEvenWhenForced(t *testing.T) {
	var se *SecretError
	d := dpDot()
	d.Trigger.FileGlobs = []string{"ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789/**"}
	if _, err := Export(d, nil, ExportOptions{Force: true}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "trigger.file_globs" {
		t.Fatalf("file glob secret: %v", err)
	} else if strings.Contains(err.Error(), "ghp_") {
		t.Fatal("the error quoted the secret")
	}
	d = dpDot()
	d.Purpose += "\nUse key sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := Export(d, nil, ExportOptions{Force: true}); !errors.As(err, &se) {
		t.Fatalf("--force exported a secret purpose: %v", err)
	}
	if _, err := Export(dpDot(), nil, ExportOptions{Force: true, Note: "key AKIAIOSFODNN7EXAMPLE"}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "note" {
		t.Fatalf("note secret: %v", err)
	}
}

func TestExportRefusesMemoryThatMustNotTravel(t *testing.T) {
	d := dpDot()
	for name, e := range map[string]memory.Entry{
		"proposed":   {Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Unapproved.", Status: memory.StatusProposed},
		"archived":   {Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Archived.", Status: memory.StatusArchived},
		"global":     {Scope: memory.ScopeGlobal, Kind: memory.KindNote, Text: "Global."},
		"preference": {Scope: memory.ScopeDot, Kind: memory.KindPreference, Text: "British spelling."},
		"other dot":  {Scope: memory.ScopeDot, Dot: "other", Kind: memory.KindNote, Text: "Another dot's."},
	} {
		if _, err := Export(d, []memory.Entry{e}, ExportOptions{Force: true}); err == nil || errors.Is(err, ErrSecretInExport) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := memory.Entry{Scope: memory.ScopeDot, Dot: d.Slug, Kind: memory.KindNote, Text: "Mine.", Status: memory.StatusActive}
	if _, err := Export(d, []memory.Entry{ok}, ExportOptions{}); err != nil {
		t.Fatalf("own active entry: %v", err)
	}
}

func TestParseRefusesWhatItCantCheck(t *testing.T) {
	good, _ := Export(dpDot(), nil, ExportOptions{Now: dpNow})
	cases := map[string]string{
		"format":     strings.Replace(string(good), "format = 1", "format = 2", 1),
		"no pack":    strings.Replace(string(good), "[pack]", "[other]", 1),
		"future key": string(good) + "\n[dot.capabilities]\nshell = true\n",
		"preference": string(good) + "\n[[memory]]\nscope = \"dot\"\nkind = \"preference\"\ntext = \"x\"\npaths = []\n",
		"global":     string(good) + "\n[[memory]]\nscope = \"global\"\nkind = \"note\"\ntext = \"x\"\npaths = []\n",
		"not toml":   "{",
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Parse(make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size: %v", err)
	}
}

func TestFetchHTTPSOnlyCappedAndReportsTheFinalURL(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("[pack]\nformat = 1\n"))
	}))
	defer plain.Close()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, srv.URL+"/new.dotpack.toml", http.StatusFound)
		case "/new.dotpack.toml":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("[pack]\nformat = 1\n"))
		case "/downgrade":
			http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
		case "/huge":
			w.Header().Set("Content-Type", "text/plain")
			w.Write(make([]byte, 50<<20))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, final, err := Fetch(ctx, srv.Client(), srv.URL+"/old", MaxBytes)
	if err != nil || final != srv.URL+"/new.dotpack.toml" || !strings.HasPrefix(string(data), "[pack]") {
		t.Fatalf("redirect: %q %q %v", data, final, err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), plain.URL+"/x", MaxBytes); !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("http URL: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/downgrade", MaxBytes); !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("redirect to http: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/huge", MaxBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("50 MB: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/json", MaxBytes); !errors.Is(err, ErrContentType) {
		t.Fatalf("json: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/missing", MaxBytes); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestFetchSendsNoCredentialsAndStaysOnTheHost(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("[pack]\nformat = 1\n"))
	}))
	defer other.Close()
	var sawCreds bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			sawCreds = true
		}
		switch r.URL.Path {
		case "/elsewhere":
			http.Redirect(w, r, other.URL+"/p.dotpack.toml", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("[pack]\nformat = 1\n"))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := srv.Client()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(srv.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc"}})
	c.Jar = jar
	if _, _, err := Fetch(ctx, c, srv.URL+"/p.dotpack.toml", MaxBytes); err != nil {
		t.Fatal(err)
	}
	if sawCreds {
		t.Fatal("the client's cookies reached the server")
	}
	withUser := strings.Replace(srv.URL, "https://", "https://me:pw@", 1) + "/p.dotpack.toml"
	if _, _, err := Fetch(ctx, srv.Client(), withUser, MaxBytes); err == nil {
		t.Fatal("a URL with credentials was fetched")
	} else if strings.Contains(err.Error(), "pw") {
		t.Fatalf("the error quoted the password: %v", err)
	}
	if sawCreds {
		t.Fatal("URL credentials reached the server")
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/elsewhere", MaxBytes); err == nil {
		t.Fatal("a redirect to another host was followed")
	}
}

func TestSelectMemoryFollowsS9(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	const pid = "g-0123456789abcdef"
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Model convention.", Source: memory.SourceModel},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindDismissed, Text: "My own rule.", Source: memory.SourceUser},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Archived finding.", Source: memory.SourceOutcome, Status: memory.StatusArchived},
		{Scope: memory.ScopeDot, Dot: "other", Kind: memory.KindNote, Text: "Another dot.", Source: memory.SourceModel},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Project decision.", Source: memory.SourceModel},
		{Scope: memory.ScopeGlobal, Dot: "security", Kind: memory.KindPreference, Text: "British spelling.", Source: memory.SourceUser},
	} {
		if _, err := st.Add(pid, e); err != nil {
			t.Fatal(err)
		}
	}
	texts := func(es []memory.Entry) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Text)
		}
		return strings.Join(out, "|")
	}
	if es, _ := SelectMemory(st, pid, "security", "", true); es != nil {
		t.Fatal("no --with-memory, no memory")
	}
	if es, _ := SelectMemory(st, pid, "security", WithDot, false); texts(es) != "Model convention." {
		t.Fatalf("dot: %s", texts(es))
	}
	if es, _ := SelectMemory(st, pid, "security", WithDot, true); texts(es) != "Model convention.|My own rule." {
		t.Fatalf("dot + user: %s", texts(es))
	}
	if es, _ := SelectMemory(st, pid, "security", WithDotProject, false); texts(es) != "Project decision.|Model convention." {
		t.Fatalf("dot+project: %s", texts(es))
	}
	if _, err := SelectMemory(st, "", "security", WithDot, false); err == nil {
		t.Fatal("no project")
	}
	if _, err := SelectMemory(st, pid, "security", "everything", false); err == nil {
		t.Fatal("bad choice")
	}
}

// ---- fix round 1

func TestParseRefusesTOMLBombsFast(t *testing.T) {
	bombs := map[string]string{
		"64 KB nested inline tables":  "x = " + strings.Repeat("{a=", 64<<10/4) + "1" + strings.Repeat("}", 64<<10/4),
		"128 KB nested inline tables": "x = " + strings.Repeat("{a=", 128<<10/4) + "1" + strings.Repeat("}", 128<<10/4),
		"64 KB dotted key":            strings.Repeat("a.", 32<<10) + "a = 1",
		"nested arrays":               "x = " + strings.Repeat("[", 60<<10) + strings.Repeat("]", 60<<10),
		"nine levels":                 "x = [[[[{a=[[[[1]]]]}]]]]",
		"nine-segment key":            "a.b.c.d.e.f.g.h.i = 1",
		"nine-segment header":         "[a.b.c.d.e.f.g.h.i]\n",
		"quoted segments":             strings.Repeat("\"a\" . ", 9) + "'b' = 1",
	}
	for name, bomb := range bombs {
		data := "[pack]\nformat = 1\n" + bomb + "\n"
		start := time.Now()
		_, err := Parse([]byte(data))
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%s: took %v", name, took)
		}
		if !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPreScanLimits(t *testing.T) {
	for _, s := range []string{
		"x = [[[[{a=[[[1]]]}]]]]",
		"a.b.c.d.e.f.g.h = 1",
		"[a.b.c.d.e.f.g.h]",
		"\"a\" . 'b' . c.d.e.f.g.h = 1",
		"x = 1.5\ny = 2026-10-21T10:00:00Z",
	} {
		if err := preScan(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{
		"x = [[[[{a=[[[[1]]]]}]]]]",
		"a.b.c.d.e.f.g.h.i = 1",
		"[a.b.c.d.e.f.g.h.i]",
		"\"a\" . 'b' . c.d.e.f.g.h.\"\"\"i\"\"\" = 1",
		"x = {a={b={c={d={e={f={g={h={i=1}}}}}}}}}",
	} {
		if err := preScan(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestEveryTemplateRoundTrips(t *testing.T) {
	for _, tpl := range dots.Templates() {
		data, err := Export(tpl, dpMem(), ExportOptions{Now: dpNow, Version: "0.9.0"})
		if err != nil {
			t.Fatalf("%s: %v", tpl.Slug, err)
		}
		p, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", tpl.Slug, err)
		}
		if _, _, err := Prepare(p, nil); err != nil {
			t.Fatalf("%s: %v", tpl.Slug, err)
		}
	}
}

func TestPreScanSkipsStringsAndComments(t *testing.T) {
	d := dpDot()
	d.Purpose = "Nested {{{{{{{{{{ [[[[[[[[[[ a.b.c.d.e.f.g.h.i.j \"quoted\" ''' \\ \"\"\" #not a comment"
	mem := []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Use a.b.c.d.e.f.g.h.i.j and {{{{{{{{{{ with \"quotes\" and \\ backslashes # too"}}
	data, err := Export(d, mem, ExportOptions{Now: dpNow, Note: "[[[[[[[[[[ {{{{{{{{{{"})
	if err != nil {
		t.Fatal(err)
	}
	extra := "# {{{{{{{{{{ [[[[[[[[[[ a.b.c.d.e.f.g.h.i.j\n" + string(data) + "\n# trailing ]]]]]]]]]] }}}}}}}}}}\n"
	p, err := Parse([]byte(extra))
	if err != nil {
		t.Fatal(err)
	}
	if p.Dot.Purpose != d.Purpose || p.Memory[0].Text != mem[0].Text {
		t.Fatalf("strings changed:\n%q\n%q", p.Dot.Purpose, p.Memory[0].Text)
	}
	lit := "[pack]\nformat = 1\nnote = 'C:\\{{{{{{{{{{'\n[dot]\nname = '''multi {{{{{{{{{\n[[[[[[[[[ line'''\n"
	if err := preScan(lit); err != nil {
		t.Fatalf("literal strings counted: %v", err)
	}
}

func TestParseListsAFewSanitizedUnknownKeys(t *testing.T) {
	good, _ := Export(dpDot(), nil, ExportOptions{Now: dpNow})
	data := string(good) + "\n[dot.zz]\n\"\\u202eevil\\u001b\" = 1\nk2 = 2\nk3 = 3\nk4 = 4\nk5 = 5\nk6 = 6\nk7 = 7\n"
	_, err := Parse([]byte(data))
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("err %v", err)
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\u202e\x1b") {
		t.Fatalf("unsanitized key in %q", msg)
	}
	if strings.Contains(msg, "k6") || strings.Contains(msg, "k7") || !strings.Contains(msg, "more") {
		t.Fatalf("lists too many keys: %q", msg)
	}
}

func TestExportLeavesOutAbsolutePaths(t *testing.T) {
	d := dpDot()
	d.Schedule.Projects = []string{dots.ScheduleAttached, "/Users/me/secret-client", `C:\work\client`}
	mem := []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Paths.",
		Paths: []string{"/etc/hosts", "~/notes/x.md", `C:\Users\me\x`, "c:/Users/me/y", `\\server\share\z`, "//server/share", "src/**", "docs/a.md"}}}
	data, err := Export(d, mem, ExportOptions{Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"/Users/me", "client", "/etc/hosts", "~/notes", "server", "Users"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("%q leaked:\n%s", leak, data)
		}
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Memory[0].Paths, ","); got != "src/**,docs/a.md" {
		t.Fatalf("paths %s", got)
	}
	got, cs, err := Prepare(p, nil)
	if err != nil || strings.Join(got.Schedule.Projects, ",") != dots.ScheduleAttached || strings.Contains(clampNames(cs), dots.CapProjects) {
		t.Fatalf("projects %v %v %v", got.Schedule.Projects, cs, err)
	}
}

func dpPackWith(t *testing.T, tail string) string {
	t.Helper()
	good, err := Export(dpDot(), nil, ExportOptions{Now: dpNow, Note: "n", Version: "0.9.0"})
	if err != nil {
		t.Fatal(err)
	}
	return string(good) + tail
}

func memBlock(scope, text, paths string) string {
	return "\n[[memory]]\nscope = \"" + scope + "\"\nkind = \"note\"\ntext = \"" + text + "\"\npaths = [" + paths + "]\n"
}

func TestParseRefusesUnsafeTextInMetaAndMemory(t *testing.T) {
	base := dpPackWith(t, "")
	cases := map[string]string{
		"OSC 52 note":        strings.Replace(base, `note = "n"`, `note = "\u001b]52;c;ZXZpbA==\u0007"`, 1),
		"CSI version":        strings.Replace(base, `herdr_dots_version = "0.9.0"`, `herdr_dots_version = "\u001b[2J"`, 1),
		"newline version":    strings.Replace(base, `herdr_dots_version = "0.9.0"`, `herdr_dots_version = "0.9\n0"`, 1),
		"RLO memory text":    base + memBlock("dot", `a \u202e b`, ""),
		"tag memory text":    base + memBlock("dot", `a \U000E0041 b`, ""),
		"ESC in memory text": base + memBlock("dot", `a \u001b b`, ""),
		"OSC path":           base + memBlock("dot", "ok", `"src/\u001b]8;;http://x\u0007a"`),
		"absolute path":      base + memBlock("dot", "ok", `"/etc/passwd"`),
		"drive path":         base + memBlock("dot", "ok", `"D:/x"`),
		"eleven paths":       base + memBlock("dot", "ok", strings.Repeat(`"a",`, 11)),
		"long path":          base + memBlock("dot", "ok", `"`+strings.Repeat("a", 257)+`"`),
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := base + memBlock("dot", `line one\nline two\twith a tab`, `"`+strings.Repeat("a", 256)+`"`+strings.Repeat(`,"b"`, 9))
	if p, err := Parse([]byte(ok)); err != nil || p.Memory[0].Text != "line one\nline two\twith a tab" {
		t.Fatalf("multiline memory text: %v", err)
	}
}

func TestParseCapsMemoryPerScope(t *testing.T) {
	var tail strings.Builder
	for i := 0; i < memory.MaxPendingImports; i++ {
		tail.WriteString(memBlock("dot", "d", ""))
		tail.WriteString(memBlock("project", "p", ""))
	}
	if p, err := Parse([]byte(dpPackWith(t, tail.String()))); err != nil || len(p.Memory) != 2*memory.MaxPendingImports {
		t.Fatalf("at the cap: %v", err)
	}
	tail.WriteString(memBlock("dot", "one too many", ""))
	_, err := Parse([]byte(dpPackWith(t, tail.String())))
	if !errors.Is(err, ErrFormat) || !strings.Contains(err.Error(), "51") {
		t.Fatalf("over the cap: %v", err)
	}
	var mem []memory.Entry
	for i := 0; i <= memory.MaxPendingImports; i++ {
		mem = append(mem, memory.Entry{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "x"})
	}
	if _, err := Export(dpDot(), mem, ExportOptions{}); err == nil || !strings.Contains(err.Error(), "51") {
		t.Fatalf("export over the cap: %v", err)
	}
}

func TestParseReportsANewerFormatFirst(t *testing.T) {
	data := strings.Replace(dpPackWith(t, "\n[dot.sandbox]\nshell = true\n"), "format = 1", "format = 2", 1)
	_, err := Parse([]byte(data))
	if !errors.Is(err, ErrFormat) || !strings.Contains(err.Error(), "format 2 (this version reads format 1)") {
		t.Fatalf("err %v", err)
	}
}

func TestExportScansSanitizedAndRedactShapes(t *testing.T) {
	var se *SecretError
	split := []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "token ghp_aBcDeFgHiJk\x1bLmNoPqRsTuVwXyZ0123456789"}}
	if _, err := Export(dpDot(), split, ExportOptions{}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "memory 1" {
		t.Fatalf("control-split key: %v", err)
	}
	redactOnly := "send x-api-key: abc123z"
	if _, err := Export(dpDot(), nil, ExportOptions{Note: redactOnly}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "note" {
		t.Fatalf("note: %v", err)
	}
	if _, err := Export(dpDot(), nil, ExportOptions{Version: redactOnly}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "version" {
		t.Fatalf("version: %v", err)
	}
	mem := []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: redactOnly}}
	if _, err := Export(dpDot(), mem, ExportOptions{}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "memory 1" {
		t.Fatalf("memory: %v", err)
	}
}

func TestExportRefusesUnsafeNoteAndVersion(t *testing.T) {
	for _, o := range []ExportOptions{{Note: "\x1b]52;c;ZXZpbA==\x07"}, {Version: "\x1b[2J"}, {Note: "a\u202eb"}} {
		if _, err := Export(dpDot(), nil, o); err == nil {
			t.Errorf("%+v exported", o)
		}
	}
}
