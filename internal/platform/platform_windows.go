//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DevNull is the null device, for git's -c core.hooksPath.
const DevNull = "NUL"

// HomeEnv is the variable that names the user's home directory, the one
// os.UserHomeDir and child CLIs (codex) read.
const HomeEnv = "USERPROFILE"

func herdrConfigDir(getenv func(string) string, home string) string {
	return windowsHerdrConfigDir(getenv, home)
}

// EnvKey is the form environment variable names are compared in: upper case
// on Windows, where Herdr_Pane_Id and HERDR_PANE_ID are the same variable.
func EnvKey(name string) string { return strings.ToUpper(name) }

// LookPath finds a program on PATH, preferring name.exe in any PATH directory
// over a .cmd/.bat shim found earlier (exec.LookPath picks by directory
// first). Names with a directory or an extension go straight to exec.LookPath.
func LookPath(name string) (string, error) {
	if strings.ContainsAny(name, `/\:`) || filepath.Ext(name) != "" {
		return exec.LookPath(name)
	}
	if p := findExecutable(name, filepath.SplitList(os.Getenv("PATH")), ".exe", isRegularFile); p != "" {
		return p, nil
	}
	return exec.LookPath(name)
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func comspec() string {
	if c := os.Getenv("ComSpec"); c != "" {
		return c
	}
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}

// Command builds the exec.Cmd for name. A .cmd/.bat shim runs through
// cmd.exe with a command line built by BatchCmdLine, which refuses unsafe
// arguments and over-long lines before anything starts.
func Command(name string, args []string) (*exec.Cmd, error) {
	return CommandContext(context.Background(), name, args)
}

// CommandContext is Command bound to ctx (exec.CommandContext).
func CommandContext(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	path, err := LookPath(name)
	if err != nil {
		return exec.CommandContext(ctx, name, args...), nil // Start reports "not found", as on Unix
	}
	if !IsBatchFile(path) {
		return exec.CommandContext(ctx, path, args...), nil
	}
	shell := comspec()
	line, err := BatchCmdLine(shell, path, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return cmd, nil
}

// HideWindow keeps a console child of the (windowless) engine from opening a
// console window.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}

const detachFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW

// StartDetached starts cmd with its own hidden console and process group, so
// it survives the hook or pane that launched it and its own console children
// never open windows. It first asks to break away from any job the caller is
// in; a job that forbids breakaway makes CreateProcess fail with access
// denied, and the start is retried inside the job.
func StartDetached(cmd *exec.Cmd) (*os.Process, error) {
	again := exec.Command(cmd.Path, cmd.Args[1:]...)
	again.Dir, again.Env = cmd.Dir, cmd.Env
	again.Stdin, again.Stdout, again.Stderr = cmd.Stdin, cmd.Stdout, cmd.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachFlags | windows.CREATE_BREAKAWAY_FROM_JOB, HideWindow: true}
	err := cmd.Start()
	if err == nil {
		return cmd.Process, nil
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil, err
	}
	again.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachFlags, HideWindow: true}
	if err := again.Start(); err != nil {
		return nil, err
	}
	return again.Process, nil
}

// Tree is a started child process together with everything it spawns: they
// all live in one Job Object.
type Tree struct{ job windows.Handle }

// StartTree starts cmd suspended, puts it in a new Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, then resumes it, so no grandchild can
// escape the job. Kill terminates the job; Close (and an engine crash, which
// closes the handle) kills whatever is still running in it.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	HideWindow(cmd)
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	fail := func(err error) (*Tree, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		windows.CloseHandle(job)
		return nil, err
	}
	pid := uint32(cmd.Process.Pid)
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fail(fmt.Errorf("open child process: %w", err))
	}
	err = windows.AssignProcessToJobObject(job, h)
	windows.CloseHandle(h)
	if err != nil {
		return fail(fmt.Errorf("assign child to job: %w", err))
	}
	if err := resumeProcess(pid); err != nil {
		return fail(fmt.Errorf("resume child: %w", err))
	}
	return &Tree{job: job}, nil
}

// resumeProcess resumes the suspended primary thread of pid.
func resumeProcess(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err = windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(th)
		windows.CloseHandle(th)
		return err
	}
	return errors.New("primary thread not found")
}

// Kill terminates every process in the job.
func (t *Tree) Kill() error { return windows.TerminateJobObject(t.job, 1) }

// Close closes the job handle; KILL_ON_JOB_CLOSE ends anything still running.
func (t *Tree) Close() error { return windows.CloseHandle(t.job) }

// ErrConnRefused is the error a refused dial carries on this OS, for code
// that keeps an error's identity while dropping its text (httpx scrubbing).
var ErrConnRefused error = windows.WSAECONNREFUSED

// ConnRefused reports whether a dial failed because nothing listens there
// (Winsock's WSAECONNREFUSED, which syscall.ECONNREFUSED doesn't match).
func ConnRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
