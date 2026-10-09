package council

import (
	"regexp"
	"strings"
)

// StanceMarker starts the round-2 line of a member's card part:
// "↳ ADD: ", "↳ OBJECT: " or "↳ AGREE: ".
func StanceMarker(stance string) string { return "↳ " + strings.ToUpper(stance) + ": " }

var stanceMarkerRe = regexp.MustCompile(`(?m)^[ \t]*↳ (?:ADD|OBJECT|AGREE): ?`)

// StripStanceMarkers removes the round-2 markers from a card part's body, so
// a remembered outcome never starts with "↳".
func StripStanceMarkers(body string) string {
	return strings.TrimSpace(stanceMarkerRe.ReplaceAllString(body, ""))
}

// Point is one bullet point of a council digest.
type Point struct {
	Text  string   // the bullet without its list marker and attribution
	Names []string // the attribution's member names; nil when it has none
}

var (
	bulletRe      = regexp.MustCompile(`^(?:[-*]|\d+[.)])\s+(.*)$`)
	attributionRe = regexp.MustCompile(`\s*\(([^()]*)\)[\s.]*$`)
)

// Points parses a council digest into its bullet points: "- ", "* ", "1. "
// and "1) " bullets. Header lines ("## …") and the "Not heard from" section
// are skipped. A trailing "(…)" is an attribution, and is stripped, only when
// every comma-separated name in it is one of members (display names or slugs,
// any case): "(see RFC 7519)" stays part of the text.
func Points(body string, members []string) []Point {
	known := map[string]bool{}
	for _, m := range members {
		if m = strings.ToLower(strings.TrimSpace(m)); m != "" {
			known[m] = true
		}
	}
	var out []Point
	section := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") || line == "##" {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "##")))
			continue
		}
		m := bulletRe.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(section, "not heard") {
			continue
		}
		p := Point{Text: strings.TrimSpace(m[1])}
		if a := attributionRe.FindStringSubmatchIndex(p.Text); a != nil {
			names := strings.Split(p.Text[a[2]:a[3]], ",")
			real := true
			for i, n := range names {
				names[i] = strings.TrimSpace(n)
				if !known[strings.ToLower(names[i])] {
					real = false
				}
			}
			if rest := strings.TrimSpace(p.Text[:a[0]]); real && rest != "" {
				p.Text, p.Names = rest, names
			}
		}
		if p.Text != "" && !strings.HasPrefix(p.Text, "## ") {
			out = append(out, p)
		}
	}
	return out
}

// FirstPoint is the digest's first bullet point. A digest without one gives
// its first line that is not a header; it never returns a "## " header.
func FirstPoint(body string, members []string) string {
	if ps := Points(body, members); len(ps) > 0 {
		return ps[0].Text
	}
	return firstTextLine(body)
}

// MemberPoint is the first bullet point whose attribution names the member
// (any of self: its display name and slug). ok=false means no bullet names
// it; text is then the member's own part body (fallback) without round-2
// markers. It never returns a "## " header.
func MemberPoint(body string, members, self []string, fallback string) (text string, ok bool) {
	for _, p := range Points(body, members) {
		for _, n := range p.Names {
			for _, s := range self {
				if s != "" && strings.EqualFold(n, s) {
					return p.Text, true
				}
			}
		}
	}
	fallback = StripStanceMarkers(fallback)
	if strings.HasPrefix(strings.TrimSpace(fallback), "##") {
		return firstTextLine(fallback), false
	}
	return fallback, false
}

// firstTextLine is the first non-empty line that is not a "## " header, or
// the first header's title when there is nothing else.
func firstTextLine(body string) string {
	title := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "##") {
			if title == "" {
				title = strings.TrimSpace(strings.TrimLeft(line, "#"))
			}
			continue
		}
		if line != "" {
			return line
		}
	}
	return title
}
