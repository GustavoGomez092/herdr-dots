package council

import "testing"

var members = []string{"Security", "security", "Frontend", "frontend"}

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
	if got, ok := MemberPoint(body, members, []string{"frontend", "Frontend"}, "fallback"); !ok || got != "No loading state on login" {
		t.Fatalf("frontend: %q %v", got, ok)
	}
	if got, ok := MemberPoint(body, members, []string{"security", "Security"}, "fallback"); !ok || got != "JWT stored in localStorage" {
		t.Fatalf("security: %q %v", got, ok)
	}
	if got, ok := MemberPoint(body, members, []string{"copy", "Copy"}, "Copy's own answer.\n↳ ADD: more"); ok || got != "Copy's own answer.\nmore" {
		t.Fatalf("an unnamed member falls back to its own body: %q %v", got, ok)
	}
	if got, ok := MemberPoint("## Must fix", members, []string{"security"}, "## Must fix"); ok || got != "Must fix" {
		t.Fatalf("never a header: %q %v", got, ok)
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
