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

// Member is a council member as a digest attribution may name it.
type Member struct{ Name, Slug string }

// NoPoint is what FirstPoint and MemberPoint return when there is no text
// at all to remember (a body of only "##"). Callers check for it and store
// nothing.
const NoPoint = "(no point to remember)"

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
func Points(body string, members []Member) []Point {
	known := map[string]bool{}
	for _, mb := range members {
		for _, m := range []string{mb.Name, mb.Slug} {
			if m = strings.ToLower(strings.TrimSpace(m)); m != "" {
				known[m] = true
			}
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
// its first line that is not a header. It never returns "" or a "## "
// header: a digest with no text at all gives NoPoint.
func FirstPoint(body string, members []Member) string {
	if ps := Points(body, members); len(ps) > 0 {
		return ps[0].Text
	}
	return orNoPoint(firstTextLine(body))
}

// MemberPoint is the first bullet point whose attribution names the member:
// its display name first, then its slug, but only a slug that is not another
// member's display name ("(UI)" is the member named UI, never a member whose
// slug is ui). ok=false means no bullet names it; text is then OwnText of
// the member's own part body (fallback). It never returns "" or a "## "
// header.
func MemberPoint(body string, members []Member, self Member, fallback string) (text string, ok bool) {
	ps := Points(body, members)
	for _, p := range ps {
		for _, n := range p.Names {
			if self.Name != "" && strings.EqualFold(n, self.Name) {
				return p.Text, true
			}
		}
	}
	slugOK := self.Slug != ""
	for _, m := range members {
		if strings.EqualFold(m.Name, self.Slug) && !strings.EqualFold(m.Name, self.Name) {
			slugOK = false // the slug is another member's display name
		}
	}
	for _, p := range ps {
		for _, n := range p.Names {
			if slugOK && strings.EqualFold(n, self.Slug) {
				return p.Text, true
			}
		}
	}
	return OwnText(fallback), false
}

// OwnText is what a member's own card part contributes to memory: the body
// without round-2 markers, or its first line of text when it starts with a
// "## " header. It never returns "" or a "## " header: a body with no text
// gives NoPoint.
func OwnText(body string) string {
	body = StripStanceMarkers(body)
	if first, _, _ := strings.Cut(body, "\n"); isHeader(first) {
		body = firstTextLine(body)
	}
	return orNoPoint(body)
}

var headerRe = regexp.MustCompile(`^#{1,6}(?:\s|$)`)

// isHeader reports a Markdown header line ("## Must fix", "##").
func isHeader(line string) bool { return headerRe.MatchString(strings.TrimSpace(line)) }

func orNoPoint(s string) string {
	if s = strings.TrimSpace(s); s == "" || isHeader(s) {
		return NoPoint
	}
	return s
}

// firstTextLine is the first non-empty line that is not a "## " header, or
// the first header's title when there is nothing else.
func firstTextLine(body string) string {
	title := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if isHeader(line) {
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
