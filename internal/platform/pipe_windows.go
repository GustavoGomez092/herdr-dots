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
// It fails if the name already exists (go-winio v0.6.2 creates the first
// instance with NtCreateNamedPipeFile and FILE_CREATE), so a pipe another
// user created first is never shared.
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
// elevated. The owner must therefore be exactly the current user's SID.
// Administrators is not accepted for pipes, elevated or not (unlike files,
// see ownerAccepted), as is SYSTEM and every other account. See
// pipeOwnerAccepted.
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
	var name string
	if owner != nil {
		name = owner.String()
	}
	if !pipeOwnerAccepted(name, me.String()) {
		return pipeOwnerError(name)
	}
	return nil
}
