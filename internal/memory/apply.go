package memory

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/textutil"
)

// ApplyOptions widens what Apply accepts. Research is set only for a dot's
// idle research pass: its ops may also add research and question entries.
type ApplyOptions struct {
	Research bool
}

// ApplyResult counts what Store.Apply did. Rejected holds one reason per
// refused op and never the op's text (it may hold a secret).
type ApplyResult struct {
	Added, Updated, Forgotten int
	Rejected                  []string
}

func (r ApplyResult) Applied() int { return r.Added + r.Updated + r.Forgotten }

// Apply validates and applies a dot's model-written memory ops (spec §6.5):
//   - text that looks like a secret, is empty or is too long is refused;
//   - add: dot scope lands active; project scope lands proposed (active with
//     AutoApproveProject); only convention, decision, finding and note (plus
//     research and question with ApplyOptions{Research: true}); never
//     dismissed, preference or global;
//   - update/forget: only the dot's own model-written dot entries and its own
//     still-proposed project entries.
//
// The error is only for store failures (lock timeout, newer file version);
// per-op problems go to Rejected.
func (s *Store) Apply(p, dot string, ops []backend.MemoryOp, ev Evidence, opts ...ApplyOptions) (ApplyResult, error) {
	var res ApplyResult
	kinds := ModelKinds
	for _, o := range opts {
		if o.Research {
			kinds = append(slices.Clone(ModelKinds), ResearchKinds...)
		}
	}
	if len(ops) > backend.MaxPassOps {
		ops = ops[:backend.MaxPassOps]
	}
	for i, op := range ops {
		err := s.applyOne(p, dot, op, ev, kinds, &res)
		switch {
		case err == nil:
		case errors.Is(err, ErrLocked), errors.Is(err, ErrVersion):
			return res, err
		default:
			res.Rejected = append(res.Rejected, fmt.Sprintf("op %d (%s): %v", i+1, opLabel(op.Op), err))
		}
	}
	return res, nil
}

// opLabel names an op for a rejection reason; model-chosen strings never
// appear in reasons.
func opLabel(op string) string {
	switch op {
	case "add", "update", "forget":
		return op
	}
	return "unknown op"
}

func (s *Store) applyOne(p, dot string, op backend.MemoryOp, ev Evidence, kinds []Kind, res *ApplyResult) error {
	// Scan what is stored: prepare strips control characters, which could
	// otherwise split a secret and hide it from the scan.
	text := strings.TrimSpace(textutil.SanitizeModelText(op.Text))
	paths := sanitizePaths(op.Paths)
	if op.Op == "add" || op.Op == "update" {
		if found, kind := ScanSecrets(text + " " + strings.Join(paths, " ")); found {
			return fmt.Errorf("%w (%s)", ErrSecret, kind)
		}
	}
	switch op.Op {
	case "add":
		sc := Scope(op.Scope)
		if sc == "" {
			sc = ScopeDot
		}
		if sc != ScopeDot && sc != ScopeProject {
			return fmt.Errorf("%w: models can't write that scope", ErrDenied)
		}
		k := Kind(op.Kind)
		if k == "" {
			k = KindNote
		}
		if !slices.Contains(kinds, k) {
			return fmt.Errorf("%w: models can't write that kind of entry", ErrDenied)
		}
		e := Entry{Scope: sc, Dot: dot, Kind: k, Text: text, Paths: paths, Status: StatusActive, Source: SourceModel, Evidence: ev}
		if sc == ScopeProject && !s.Limits.AutoApproveProject {
			e.Status = StatusProposed
		}
		var r insertResult
		err := s.mutate(p, sc, dotFor(sc, dot), func(f *scopeFile) error {
			var err error
			_, r, err = s.insert(f, e)
			return err
		})
		if err != nil {
			return err
		}
		if r == merged {
			res.Updated++
		} else {
			res.Added++
		}
		return nil
	case "update", "forget":
		ref, err := ParseRef(op.ID, dot)
		if err != nil {
			return fmt.Errorf("%w: invalid id", ErrInvalid)
		}
		if ref.Scope == ScopeGlobal {
			return fmt.Errorf("%w: global memory is written by the developer only", ErrDenied)
		}
		err = s.mutate(p, ref.Scope, ref.Dot, func(f *scopeFile) error {
			i := f.find(ref.ID)
			if i < 0 {
				return fmt.Errorf("%w: %s", ErrNotFound, ref)
			}
			x := f.Entries[i]
			if x.Source != SourceModel || x.Dot != dot || (x.Scope == ScopeProject && x.Status != StatusProposed) {
				return fmt.Errorf("%w: %s", ErrDenied, ref)
			}
			if op.Op == "forget" {
				f.Entries = slices.Delete(f.Entries, i, i+1)
				return nil
			}
			x.Text = text
			if len(paths) > 0 {
				x.Paths = paths
			}
			x, err := s.prepare(x)
			if err != nil {
				return err
			}
			x.UpdatedAt = s.now()
			f.Entries[i] = x
			return nil
		})
		if err != nil {
			return err
		}
		if op.Op == "forget" {
			res.Forgotten++
		} else {
			res.Updated++
		}
		return nil
	}
	return fmt.Errorf("%w: unknown op", ErrInvalid)
}

// sanitizePaths strips control characters from model-written paths before
// they are scanned and stored, like the text: a control character could
// otherwise split a secret and hide it from the scan.
func sanitizePaths(in []string) []string {
	var out []string
	for _, p := range in {
		if p = strings.TrimSpace(textutil.SanitizeModelText(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
