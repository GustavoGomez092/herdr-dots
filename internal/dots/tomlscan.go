package dots

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pre-scan limits: no dot or pack needs deeper nesting or longer key paths,
// and the TOML decoder takes seconds and gigabytes on files that go far past
// them (nested inline tables, a 32k-segment dotted key).
const (
	maxTOMLNesting     = 8
	maxTOMLKeySegments = 8
)

// PreScanTOML refuses bracket/brace nesting deeper than 8 and dotted key
// paths longer than 8 parts, in one pass that skips comments and all four
// kinds of TOML string. It leaves every other syntax question to the
// decoder. Run it before decoding any TOML that came from someone else
// (project dots, user dots, dot packs).
func PreScanTOML(data []byte) error {
	s := string(data)
	depth, segs := 0, 0
	inKey, afterDot := false, false // a run of key segments; the last token was "."
	segment := func() error {
		if inKey && afterDot {
			segs++
		} else {
			segs = 1
		}
		inKey, afterDot = true, false
		if segs > maxTOMLKeySegments {
			return fmt.Errorf("a key path has more than %d parts", maxTOMLKeySegments)
		}
		return nil
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '#':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return nil
			}
			i += j
			continue
		case c == '"' || c == '\'':
			end := tomlStringEnd(s, i)
			if end < 0 {
				return nil // unterminated: the decoder reports it
			}
			i = end
			if err := segment(); err != nil {
				return err
			}
			continue
		case c == '[' || c == '{':
			depth++
			if depth > maxTOMLNesting {
				return fmt.Errorf("brackets or braces nest more than %d deep", maxTOMLNesting)
			}
			inKey, afterDot = false, false
		case c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
			inKey, afterDot = false, false
		case c == '.':
			afterDot = true
		case c == ' ' || c == '\t':
			// whitespace may surround the dots of a key
		case bareKeyByte(c):
			j := i
			for j < len(s) && bareKeyByte(s[j]) {
				j++
			}
			i = j
			if err := segment(); err != nil {
				return err
			}
			continue
		default:
			inKey, afterDot = false, false
		}
		i++
	}
	return nil
}

func bareKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// tomlStringEnd returns the index just past the TOML string starting at s[i]
// (basic, literal, or either multi-line form), or -1 when it doesn't end.
// Single-line strings end at a newline at the latest.
func tomlStringEnd(s string, i int) int {
	q := s[i]
	escapes := q == '"'
	if strings.HasPrefix(s[i:], strings.Repeat(string(q), 3)) {
		for j := i + 3; j < len(s); j++ {
			switch {
			case escapes && s[j] == '\\':
				j++
			case s[j] == q && strings.HasPrefix(s[j:], strings.Repeat(string(q), 3)):
				end := j + 3
				for k := 0; k < 2 && end < len(s) && s[end] == q; k++ {
					end++ // up to two quotes may end the content
				}
				return end
			}
		}
		return -1
	}
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] == '\n':
			return -1
		case escapes && s[j] == '\\':
			j++
		case s[j] == q:
			return j + 1
		}
	}
	return -1
}

// CleanText shows text from an untrusted file in an error: every rune that
// doesn't print as itself (controls, bidi and other format characters) as a
// \u escape, clipped so the result is at most max runes plus "…".
func CleanText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		piece := string(r)
		if !unicode.IsPrint(r) {
			piece = fmt.Sprintf("\\u%04x", r)
		}
		w := utf8.RuneCountInString(piece)
		if n+w > max {
			b.WriteString("…")
			break
		}
		n += w
		b.WriteString(piece)
	}
	return b.String()
}

// UnknownKeysText names at most five of a file's unknown keys, each cleaned
// and clipped, and counts the rest.
func UnknownKeysText(keys []string) string {
	var shown []string
	for _, k := range keys[:min(len(keys), 5)] {
		shown = append(shown, CleanText(k, 80))
	}
	more := ""
	if len(keys) > 5 {
		more = fmt.Sprintf(" and %d more", len(keys)-5)
	}
	return strings.Join(shown, ", ") + more
}

// decodeError is a TOML decode error with a cleaned, clipped message. It
// unwraps to the decoder's error, so a toml.ParseError still names its line.
type decodeError struct {
	msg string
	err error
}

func (e *decodeError) Error() string { return e.msg }
func (e *decodeError) Unwrap() error { return e.err }

// cleanDecodeErr bounds a decoder error from an untrusted file.
func cleanDecodeErr(err error) error {
	return &decodeError{msg: CleanText(err.Error(), 300), err: err}
}
