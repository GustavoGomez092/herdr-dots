//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// PipeSDDL is the security descriptor for herdr-dots' pipes: owned by the
// current user, who alone gets access.
func PipeSDDL() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("O:%[1]sD:P(A;;GA;;;%[1]s)", sid.String()), nil
}

// ListenPipe serves the named pipe name, reachable only by the current user.
// It fails if the name already exists (go-winio creates the first instance
// with FILE_FLAG_FIRST_PIPE_INSTANCE), so a pipe another user created first
// is never shared.
func ListenPipe(name string) (net.Listener, error) {
	sddl, err := PipeSDDL()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sddl})
}

// DialPipe connects to the named pipe name.
func DialPipe(ctx context.Context, name string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, name)
}

// CheckPipeOwner fails with ErrNotOwner unless the pipe behind conn belongs
// to the current user, so a pipe another user squatted under the engine's
// name is refused instead of trusted.
//
// Elevation (UAC): ListenPipe sets the owner explicitly to the user SID, so
// the engine's own pipe is owned by the user even when the engine runs
// elevated. An Administrators owner (what an elevated process gets by
// default for objects it makes without an explicit owner) is also accepted,
// but only when this process's token has Administrators enabled (elevated).
// A non-elevated process refuses an Administrators-owned pipe, because a
// more privileged principal made it. SYSTEM and other users are always
// refused. See ownerAccepted.
func CheckPipeOwner(conn net.Conn) error {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("pipe connection exposes no handle")
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read pipe owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read pipe owner: %w", err)
	}
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	if !ownerOK(owner, me) {
		return fmt.Errorf("%w: the pipe is owned by %s", ErrNotOwner, owner.String())
	}
	return nil
}
