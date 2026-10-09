//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// EditorCommand runs the user's editor command line (from %EDITOR%, which
// may carry flags such as "code --wait") on file through cmd.exe:
// <comspec> /d /s /c "<editor> "<file>"". file is a path herdr-dots made
// (a private temp file), so it holds no quote; the editor line is the user's
// own setting and runs as they wrote it.
func EditorCommand(editor, file string) *exec.Cmd {
	shell := comspec()
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + ` /d /s /c "` + editor + ` "` + file + `""`}
	return cmd
}
