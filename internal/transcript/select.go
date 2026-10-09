package transcript

type SelectOptions struct {
	Agent           string // herdr agent kind: "claude", "codex", ...
	SessionKind     string // "id" | "path"
	SessionValue    string
	ForceScrollback bool
	Home            string
	Pane            PaneReader
	PaneID          string
	ScrollbackLines int // 0 → 400
}

// Select returns the best reader for a pane: the agent's transcript file when
// it can be resolved, otherwise the scrollback reader.
func Select(o SelectOptions) Reader {
	if !o.ForceScrollback && o.SessionValue != "" {
		switch o.Agent {
		case "claude":
			if p, err := ResolveClaudePath(o.Home, o.SessionKind, o.SessionValue); err == nil {
				return ClaudeReader{Path: p}
			}
		case "codex":
			if p, err := ResolveCodexPath(o.Home, o.SessionKind, o.SessionValue); err == nil {
				return CodexReader{Path: p}
			}
		}
	}
	lines := o.ScrollbackLines
	if lines == 0 {
		lines = 400
	}
	return NewScrollback(o.Pane, o.PaneID, lines)
}
