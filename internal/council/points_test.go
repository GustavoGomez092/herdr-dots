package council

import "testing"

var (
	security = Member{Name: "Security", Slug: "security"}
	frontend = Member{Name: "Frontend", Slug: "frontend"}
	members  = []Member{security, frontend}
)

func TestFirstPointSkipsHeadersAndStripsAttribution(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"dash":              {"## Must fix\n- JWT stored in localStorage (Security, Frontend)\n## Should fix\n- No spinner (Frontend)", "JWT stored in localStorage"},
		"star":              {"## Must fix\n* JWT in localStorage. (Security)", "JWT in localStorage."},
		"numbered dot":      {"## Must fix\n1. JWT in localStorage (Security)\n2. No spinner (Frontend)", "JWT in localStorage"},
		"numbered paren":    {"## Must fix\n1) JWT in localStorage (security)", "JWT in localStorage"},
		"not attribution":   {"## Must fix\n- Validate the token expiry (see RFC 7519)", "Validate the token expiry (see RFC 7519)"},
		"mixed list kept":   {"- Use cookies (Security, see RFC 6265)", "Use cookies (Security, see RFC 6265)"},
		"no bullets":        {"## Must fix\n\nMove the token to a cookie.", "Move the token to a cookie."},
		"only a header":     {"## Must fix", "Must fix"},
		"not heard skipped": {"## Not heard from\n- Infra (timed out)\n## Consider\n- Add a spinner (Frontend)", "Add a spinner"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := FirstPoint(tc.body, members); got != tc.want {
				t.Fatalf("FirstPoint = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMemberPointSelectsTheMembersOwnBullet(t *testing.T) {
	body := "## Must fix\n- JWT stored in localStorage (Security)\n## Should fix\n1. No loading state on login (Frontend)\n2) Rate limit the login (Security, Frontend)"
	if got, ok := MemberPoint(body, members, frontend, "fallback"); !ok || got != "No loading state on login" {
		t.Fatalf("frontend: %q %v", got, ok)
	}
	if got, ok := MemberPoint(body, members, security, "fallback"); !ok || got != "JWT stored in localStorage" {
		t.Fatalf("security: %q %v", got, ok)
	}
	if got, ok := MemberPoint(body, members, Member{Name: "Copy", Slug: "copy"}, "Copy's own answer.\n↳ ADD: more"); ok || got != "Copy's own answer.\nmore" {
		t.Fatalf("an unnamed member falls back to its own body: %q %v", got, ok)
	}
	if got, ok := MemberPoint("## Must fix", members, security, "## Must fix"); ok || got != "Must fix" {
		t.Fatalf("never a header: %q %v", got, ok)
	}
}

// Re-review follow-up 1: a slug that is another member's display name never
// matches; display names are matched first.
func TestMemberPointSlugNeverStealsAnotherMembersName(t *testing.T) {
	fe, ui := Member{Name: "Frontend", Slug: "ui"}, Member{Name: "UI", Slug: "ui-2"}
	ms := []Member{fe, ui}
	body := "## Should fix\n- UI's point (UI)"
	if got, ok := MemberPoint(body, ms, fe, "Frontend's own answer."); ok || got != "Frontend's own answer." {
		t.Fatalf("Frontend (slug ui) took UI's point: %q %v", got, ok)
	}
	if got, ok := MemberPoint(body, ms, ui, "UI's own answer."); !ok || got != "UI's point" {
		t.Fatalf("UI: %q %v", got, ok)
	}
	fe2 := Member{Name: "Frontend", Slug: "fe"}
	if got, ok := MemberPoint("- by slug (fe)", []Member{fe2}, fe2, "x"); !ok || got != "by slug" {
		t.Fatalf("a slug that is no member's name still matches: %q %v", got, ok)
	}
	if got, ok := MemberPoint("- by slug (fe)\n- by name (Frontend)", []Member{fe2}, fe2, "x"); !ok || got != "by name" {
		t.Fatalf("display names are matched first: %q %v", got, ok)
	}
}

// Re-review follow-up 2: a body of only "##" or "## " never gives "".
func TestPointsAreNeverEmpty(t *testing.T) {
	for _, body := range []string{"##", "## ", "##\n## ", ""} {
		if got := FirstPoint(body, members); got != NoPoint {
			t.Errorf("FirstPoint(%q) = %q, want NoPoint", body, got)
		}
		if got, ok := MemberPoint(body, members, security, "My own answer."); ok || got != "My own answer." {
			t.Errorf("MemberPoint(%q) = %q %v, want the member's own body", body, got, ok)
		}
		if got, _ := MemberPoint(body, members, security, body); got != NoPoint {
			t.Errorf("MemberPoint(%q) with an empty own body = %q, want NoPoint", body, got)
		}
	}
}

func TestOwnText(t *testing.T) {
	for in, want := range map[string]string{
		"## Tokens\nMove the token to a cookie.": "Move the token to a cookie.",
		"#1 issue: tokens in localStorage.":      "#1 issue: tokens in localStorage.",
		"↳ ADD: use cookies.":                    "use cookies.",
		"## ":                                    NoPoint,
		"":                                       NoPoint,
	} {
		if got := OwnText(in); got != want {
			t.Errorf("OwnText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripStanceMarkers(t *testing.T) {
	for in, want := range map[string]string{
		"↳ ADD: cookies need CSRF.":                 "cookies need CSRF.",
		"JWT in localStorage.\n↳ OBJECT: no, fine.": "JWT in localStorage.\nno, fine.",
		"↳ AGREE: yes":                              "yes",
		"plain text":                                "plain text",
	} {
		if got := StripStanceMarkers(in); got != want {
			t.Errorf("StripStanceMarkers(%q) = %q, want %q", in, got, want)
		}
	}
}
