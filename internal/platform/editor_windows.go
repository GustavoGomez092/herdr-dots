//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// EditorCommand runs the user's editor command line (from %EDITOR%, which
// may carry flags such as "code --wait") on file through cmd.exe:
// "<comspec>" /d /s /c "<editor> "<file>"". file is a path herdr-dots made
// (a private temp file), so it holds no quote or %; the editor line is the
// user's own setting and runs as they wrote it.
//
// cmd.exe looks in the current directory before PATH, so a repo-local
// notepad.exe or code.cmd would win. The child therefore starts in the file's
// private temp directory, and NoDefaultCurrentDirectoryInExePath=1 stops the
// search in the current directory altogether.
func EditorCommand(editor, file string) *exec.Cmd {
	shell := comspec()
	quoted := shell
	if strings.ContainsAny(shell, " \t") {
		quoted = `"` + shell + `"`
	}
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: quoted + ` /d /s /c "` + editor + ` "` + file + `""`}
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(os.Environ(), "NoDefaultCurrentDirectoryInExePath=1")
	return cmd
}
