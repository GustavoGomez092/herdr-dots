package projectdots

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/dots"
)

// Item is one valid project dot file, trusted or pending trust.
//
// Dot and Effective are PREVIEW-ONLY: they exist so a trust preview can show
// what the file asks for and what would run. Neither may ever be run,
// attached, scheduled or merged into the engine's dots directly: an
// untrusted or changed file has both filled in. Runnable is the only way to
// get a dot to run (Task 9 must go through it).
type Item struct {
	Dot       dots.Dot     // PREVIEW-ONLY, never run: as written (project slug, Origin project, SourcePath)
	Effective dots.Dot     // PREVIEW-ONLY, never run: Dot clamped except Granted; run it only via Runnable
	Rel       string       // path from the repository root
	Sum       [32]byte     // SHA-256 of the bytes that were parsed
	Trusted   bool         // this exact content is in the trust store
	Granted   []string     // capabilities the trust entry grants (subset of Requested)
	Requested []string     // keepable capabilities the file asks for
	Clamps    []dots.Clamp // what Effective has clamped

	TrustedPath string // where the trust entry was given (display only; "" when untrusted)
}

// Runnable returns the dot to run, a copy of the clamped Effective, and only
// when this exact content is trusted. It is nil for an untrusted or changed
// file, and for every file while the trust store can't be read: such a file
// is pending trust and never runs, in the foreground or the background.
func (it Item) Runnable() *dots.Dot {
	if !it.Trusted {
		return nil
	}
	d := it.Effective
	return &d
}

// Scan is one repository's project dots.
type Scan struct {
	Root      string
	ProjectID string
	Items     []Item
	Invalid   []dots.LoadError
	Stamp     string // changes when a file or the trust store changes
}

// Find returns the item with this engine slug. The item may be pending
// trust: run it only through Item.Runnable.
func (s Scan) Find(slug string) (Item, bool) {
	for _, it := range s.Items {
		if it.Dot.Slug == slug {
			return it, true
		}
	}
	return Item{}, false
}

// ScanProject reads root's project dots (at most dots.MaxProjectFiles) and
// checks each against one snapshot of ts. A trust store that can't be read
// leaves every file untrusted and is reported once in Invalid whenever the
// repository has project dot files, even when none of them decodes. Each
// file's bytes are read once: the hash, the parsed dot and the stamp come
// from the same bytes. Errors name files from the repository root.
func ScanProject(root, projectID string, ts *TrustStore) Scan {
	files, bad := dots.ReadProjectFiles(root)
	var doc trustDoc
	var storeStamp string
	var storeErr error
	if ts != nil {
		doc, storeStamp, storeErr = ts.snapshot()
	}
	s := Scan{Root: root, ProjectID: projectID, Stamp: stampOf(files, bad, ts != nil, storeStamp)}
	for _, le := range bad {
		le.Err = plainErr(le.Err)
		s.Invalid = append(s.Invalid, le)
	}
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
		if ts != nil && storeErr == nil {
			if e, ok := doc.entry(projectID, f.Rel, it.Sum); ok {
				it.Trusted, it.TrustedPath = true, e.Path
				for _, c := range e.Capabilities {
					if slices.Contains(it.Requested, c) {
						it.Granted = append(it.Granted, c)
					}
				}
			}
		}
		it.Effective, it.Clamps = dots.ClampDot(d, it.Granted)
		s.Items = append(s.Items, it)
	}
	if storeErr != nil && len(files)+len(bad) > 0 {
		s.Invalid = append(s.Invalid, dots.LoadError{File: "trusted.json", Err: storeErr})
	}
	return s
}

// Stamp fingerprints root's project dot files by content and the trust
// store, so poll notices any edit, even one that keeps the size and
// modification time. It is bounded like a scan: at most dots.MaxProjectFiles
// files and dots.MaxProjectBytes bytes.
func Stamp(root string, ts *TrustStore) string {
	files, bad := dots.ReadProjectFiles(root)
	storeStamp := ""
	if ts != nil {
		_, storeStamp, _ = ts.snapshot()
	}
	return stampOf(files, bad, ts != nil, storeStamp)
}

func stampOf(files []dots.ProjectFile, bad []dots.LoadError, haveStore bool, storeStamp string) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s=%s;", f.Rel, Hex(Sum(f.Data)))
	}
	for _, le := range bad {
		fmt.Fprintf(&b, "!%s:%v;", le.File, plainErr(le.Err))
	}
	if haveStore {
		b.WriteString("|" + storeStamp)
	}
	return b.String()
}
