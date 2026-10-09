package pane

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

// awaySnap is snap() with proactive hints, an away digest card first and
// today's background spend.
func awaySnap() view.Snapshot {
	s := snap()
	s.TodayUSD, s.TodayRuns, s.DailyBudgetUSD = 0.37, 4, 1.00
	s.Panes[0].Dots[0].Status, s.Panes[0].Dots[0].Next = "idle", "research in 6m"
	s.Panes[0].Dots[1].Background = state.RunCommit
	s.Panes[0].Away = &view.AwayState{Since: time.Date(2026, 10, 21, 10, 12, 0, 0, time.Local), Pending: 2}
	s.Panes[0].Cards = append([]queue.Card{{
		ID: "7", Kind: queue.KindDigest, Dot: "away", DotName: "Away", Glyph: "☾", Severity: "warn", Mode: "queue", Status: queue.StatusPending,
		Body: "## Should fix\n- lodash advisory (Security)", Note: "researched while you were away", Origin: state.RunResearch, Label: "2 dots · 42 min",
		Parts: []queue.CardPart{
			{Dot: "security", DotName: "Security", Glyph: "◆", Severity: "warn", Body: "lodash 4.17.20 has a prototype pollution advisory."},
			{Dot: "frontend", DotName: "Frontend", Glyph: "▲", Severity: "info", Body: "The login form lacks autocomplete attributes."},
		},
	}, {
		ID: "8", Dot: "security", DotName: "Security", Glyph: "◆", Severity: "info", Body: "CheckCSRF has no test.", Mode: "queue",
		Status: queue.StatusPending, Origin: state.RunCommit, Label: "commit a1b2c3d",
	}}, s.Panes[0].Cards...)
	return s
}

func awayReady(fc *fakeCaller) tea.Model {
	m, _ := New("w1:p1", fc).Update(tea.WindowSizeMsg{Width: 120})
	m, _ = m.Update(SnapshotMsg(awaySnap()))
	return m
}

func TestRosterShowsProactiveHintsAndBackgroundRuns(t *testing.T) {
	v := awayReady(&fakeCaller{}).View()
	for _, want := range []string{"idle · research in 6m", "◌ reviewing commit…", "today $0.37/1.00",
		"☾ 2 research results waiting (since 10:12)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
	quiet := snap()
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(quiet))
	if v := m.View(); strings.Contains(v, "today $") || strings.Contains(v, "research results waiting") {
		t.Fatalf("nothing proactive happened today: %s", v)
	}
}

func TestDigestAndBackgroundCardHeaders(t *testing.T) {
	m := awayReady(&fakeCaller{})
	v := m.View()
	for _, want := range []string{"☾ WARN  Away · 2 dots · 42 min", "researched while you were away", "◆ INFO  Security · commit a1b2c3d", "space expand"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "prototype pollution advisory") {
		t.Fatal("parts stay hidden until expanded")
	}
	v = press(m, " ").View()
	for _, want := range []string{"◆ Security  WARN", "prototype pollution advisory", "▲ Frontend  INFO", "autocomplete attributes"} {
		if !strings.Contains(v, want) {
			t.Errorf("expanded view missing %q\n%s", want, v)
		}
	}
}

// Like a council card, a digest is remembered as a project convention only
// (the engine refuses the other kinds).
func TestDigestRemembersAsAConventionOnly(t *testing.T) {
	v := press(awayReady(&fakeCaller{}), "M").View()
	if !strings.Contains(v, "c project convention · esc cancel") || strings.Contains(v, "f accepted finding") {
		t.Fatalf("remember prompt for a digest:\n%s", v)
	}
}

func TestFeedShowsBackgroundRunDetail(t *testing.T) {
	s := snap()
	s.Panes[0].Feed = []state.FeedEntry{
		{Dot: "security", Outcome: state.OutcomeResearch, Detail: "research: nothing new · cost=$0.0100 tokens=10/5"},
		{Dot: "tests", Outcome: state.OutcomeCommit, Detail: "commit a1b2c3d: CheckCSRF has no test. · cost=?"},
	}
	m, _ := New("w1:p1", &fakeCaller{}).Update(tea.WindowSizeMsg{Width: 100})
	m, _ = m.Update(SnapshotMsg(s))
	v := press(m, "f").View()
	for _, want := range []string{"research: nothing new", "commit a1b2c3d: CheckCSRF has no test."} {
		if !strings.Contains(v, want) {
			t.Errorf("feed missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "cost=") {
		t.Errorf("the pane drops the cost tail:\n%s", v)
	}
}
