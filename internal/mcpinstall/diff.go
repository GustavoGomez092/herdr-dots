package mcpinstall

import "strings"

// diffContext is how many unchanged lines show around each change.
const diffContext = 2

// Diff renders a line diff of before → after: "- " removed, "+ " added and
// "  " unchanged lines near a change; "  …" marks skipped lines.
func Diff(before, after string) string {
	a, b := splitLines(before), splitLines(after)
	if len(a)*len(b) > 4_000_000 { // far bigger than any config file
		var out strings.Builder
		for _, l := range a {
			out.WriteString("- " + l + "\n")
		}
		for _, l := range b {
			out.WriteString("+ " + l + "\n")
		}
		return out.String()
	}
	// lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', a[i]}) // removals print before additions
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	near := make([]bool, len(ops))
	for k, o := range ops {
		if o.kind == ' ' {
			continue
		}
		for d := max(0, k-diffContext); d <= min(len(ops)-1, k+diffContext); d++ {
			near[d] = true
		}
	}
	var out strings.Builder
	skipped := false
	for k, o := range ops {
		if !near[k] {
			skipped = true
			continue
		}
		if skipped && out.Len() > 0 {
			out.WriteString("  …\n")
		}
		skipped = false
		out.WriteString(string(o.kind) + " " + o.text + "\n")
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
