package paths

import "path/filepath"

// DailyFile counts background runs and their cost per local day. Like memory
// it is shared by every named Herdr session, so it is not under SessionDir.
func (p Paths) DailyFile() string { return filepath.Join(p.StateDir, "daily.json") }

// InboxFile holds scheduled results for projects with no attached pane,
// shared by every Herdr session.
func (p Paths) InboxFile() string { return filepath.Join(p.StateDir, "inbox.json") }
