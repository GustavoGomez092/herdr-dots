package projectdots

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/dots"
)

// Item is one valid project dot file.
type Item struct {
	Dot       dots.Dot     // as written (project slug, Origin project, SourcePath)
	Effective dots.Dot     // what runs: Dot clamped except Granted
	Rel       string       // path from the repository root
	Sum       [32]byte     // SHA-256 of the bytes that were parsed
	Trusted   bool         // this exact content is in the trust store
	Granted   []string     // capabilities the trust entry grants (subset of Requested)
	Requested []string     // keepable capabilities the file asks for
	Clamps    []dots.Clamp // what Effective has clamped
}

// Runnable returns the dot to run: the clamped Effective, and only when this
// exact content is trusted. An untrusted or changed file is pending trust
// and never runs, in the foreground or the background.
func (it Item) Runnable() (dots.Dot, bool) {
	if !it.Trusted {
		return dots.Dot{}, false
	}
	return it.Effective, true
}

// Scan is one repository's project dots.
type Scan struct {
	Root      string
	ProjectID string
	Items     []Item
	Invalid   []dots.LoadError
	Stamp     string // changes when a file or the trust store changes
}

// Find returns the item with this engine slug.
func (s Scan) Find(slug string) (Item, bool) {
	for _, it := range s.Items {
		if it.Dot.Slug == slug {
			return it, true
		}
	}
	return Item{}, false
}

// ScanProject reads root's project dots and checks each against ts. A trust
// store that can't be read leaves every file untrusted and is reported in
// Invalid. Each file's bytes are read once: the hash and the parsed dot come
// from the same bytes. Errors name files from the repository root.
func ScanProject(root, projectID string, ts *TrustStore) Scan {
	files, bad := dots.ReadProjectFiles(root)
	// The stamp comes from the same bytes, so a later edit always changes it.
	s := Scan{Root: root, ProjectID: projectID, Stamp: stampOf(files, bad, ts)}
	for _, le := range bad {
		le.Err = plainErr(le.Err)
		s.Invalid = append(s.Invalid, le)
	}
	trustBroken := false
	for _, f := range files {
		d, err := dots.DecodeProject(f, projectID)
		if err != nil {
			le := dots.LoadError{File: f.Rel, Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			s.Invalid = append(s.Invalid, le)
			continue
		}
		it := Item{Dot: d, Rel: f.Rel, Sum: Sum(f.Data), Requested: dots.Requested(d)}
		if ts != nil {
			ok, caps, err := ts.Check(projectID, f.Rel, it.Sum)
			if err != nil && !trustBroken {
				trustBroken = true
				s.Invalid = append(s.Invalid, dots.LoadError{File: "trusted.json", Err: err})
			}
			if ok && err == nil {
				it.Trusted = true
				for _, c := range caps {
					if slices.Contains(it.Requested, c) {
						it.Granted = append(it.Granted, c)
					}
				}
			}
		}
		it.Effective, it.Clamps = dots.ClampDot(d, it.Granted)
		s.Items = append(s.Items, it)
	}
	return s
}

// Stamp fingerprints root's project dot files by content (each is at most
// 64 KB, and a repository ships a handful) and the trust store, so poll
// notices any edit, even one that keeps the size and modification time.
func Stamp(root string, ts *TrustStore) string {
	files, bad := dots.ReadProjectFiles(root)
	return stampOf(files, bad, ts)
}

func stampOf(files []dots.ProjectFile, bad []dots.LoadError, ts *TrustStore) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s=%s;", f.Rel, Hex(Sum(f.Data)))
	}
	for _, le := range bad {
		fmt.Fprintf(&b, "!%s;", le.File)
	}
	if ts != nil {
		b.WriteString("|" + ts.stamp())
	}
	return b.String()
}
