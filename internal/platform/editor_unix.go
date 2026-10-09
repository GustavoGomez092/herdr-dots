//go:build unix

package platform

import "os/exec"

// EditorCommand runs the user's editor command line (from $EDITOR, which may
// carry flags such as "code --wait") on file through the shell: sh -c
// '<editor> "$1"' sh <file>. file is passed as an argument, never parsed.
func EditorCommand(editor, file string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$1"`, "sh", file)
}
