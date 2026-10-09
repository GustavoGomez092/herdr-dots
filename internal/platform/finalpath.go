package platform

import (
	"errors"
	"path"
	"strings"
)

// ErrReparsePoint means a path names a symlink, junction, mount point or
// other reparse point where a plain file or directory was required.
var ErrReparsePoint = errors.New("is a symlink or reparse point")

// The rules below take plain strings so every OS can unit-test them; the
// Windows code feeds them what GetFinalPathNameByHandle and
// GetFileInformationByHandleEx return.

const (
	fileAttributeReparsePoint = 0x400      // FILE_ATTRIBUTE_REPARSE_POINT
	ioReparseTagDedup         = 0x80000013 // IO_REPARSE_TAG_DEDUP
)

// reparseRefused reports whether a handle with these attributes and reparse
// tag must be refused: any reparse point (symlink, junction, mount point,
// cloud placeholder) except a deduplicated file, which Windows and Go treat
// as a regular file.
func reparseRefused(attrs, tag uint32) bool {
	return attrs&fileAttributeReparsePoint != 0 && tag != ioReparseTagDedup
}

// normalizeFinalPath puts a Windows path in one comparable form: no \\?\ or
// \\?\UNC\ prefix, backslashes only, "." and ".." resolved, no trailing
// separator except on a drive root (C:\).
func normalizeFinalPath(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		p = `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		p = p[len(`\\?\`):]
	}
	var vol, rest string
	switch {
	case strings.HasPrefix(p, `\\`):
		// \\server\share is the volume.
		parts := strings.SplitN(p[2:], `\`, 3)
		vol = `\\` + strings.Join(parts[:min(2, len(parts))], `\`)
		if len(parts) == 3 {
			rest = parts[2]
		}
	case len(p) >= 2 && p[1] == ':':
		vol, rest = p[:2], p[2:]
	default:
		rest = p
	}
	clean := path.Clean("/" + strings.ReplaceAll(rest, `\`, "/"))
	if clean == "/" && strings.HasPrefix(vol, `\\`) {
		return vol
	}
	return vol + strings.ReplaceAll(clean, "/", `\`)
}

// volumeRoot reports whether p, read as a Windows path, is a drive or share
// root (C:\, C:, \\?\C:\, \\server\share).
func volumeRoot(p string) bool {
	if p == "" {
		return false
	}
	n := normalizeFinalPath(p)
	if len(n) == 3 && n[1] == ':' && n[2] == '\\' {
		return true
	}
	return strings.HasPrefix(n, `\\`) && strings.Count(n[2:], `\`) == 1
}

// finalPathIs reports whether final (a handle's final path) names dir\rel,
// compared without case, as NTFS does. dir may itself be a \\?\ path.
func finalPathIs(final, dir, rel string) bool {
	if final == "" || dir == "" {
		return false
	}
	return strings.EqualFold(normalizeFinalPath(final), normalizeFinalPath(dir+`\`+rel))
}
