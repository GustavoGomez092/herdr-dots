package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/ui/style"
)

// The agent/CI flag is drawn in the warning style, outside the dim style, and
// stays first; the other flags stay dim.
func TestAgentConfigFlagUsesTheWarningStyleNotDim(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	c := queue.Card{Kind: queue.KindPatch, PatchFlags: []string{"agent/CI config", "binary"}}
	got := patchDetail(c, 80)
	lead := style.Severity("warn").Render("  [agent/CI config]")
	if !strings.HasPrefix(got, lead) {
		t.Fatalf("flag not in the warning style first: %q want prefix %q", got, lead)
	}
	if strings.Contains(got, style.Dim.Render("  [agent/CI config]")) || !strings.Contains(got, style.Dim.Render(" [binary]")) {
		t.Fatalf("flag is dimmed or the others are not: %q", got)
	}
}
